//go:generate ../../../tools/readme_config_includer/generator
package snmp

import (
	_ "embed"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/snmp"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// snmp configs
type SharedConfig struct {
	// The SNMP agent to query. Format is [SCHEME://]ADDR[:PORT] (e.g.
	// udp://1.2.3.4:161).  If the scheme is not specified then "udp" is used.
	Agents []string `toml:"agents" json:"agents"`

	// The tag used to name the agent host
	AgentHostTag string `toml:"agent_host_tag" json:"agent_host_tag"`

	snmp.ClientConfig

	Tables []snmp.Table `toml:"table" json:"table"`

	// Name & Fields are the elements of a Table.
	// Telegraf chokes if we try to embed a Table. So instead we have to embed the
	// fields of a Table, and construct a Table during runtime.
	Name   string       `toml:"name" json:"name"`
	Fields []snmp.Field `toml:"field" json:"field"`

	connectionCache []snmp.Connection
	translator      snmp.Translator
}

type ConfigInjector struct {
	Type     string `toml:"type"` // Type of the injector, e.g., "fileInjector", "apiInjector"
	FilePath string `toml:"path"` // Path to the config file (only used for fileInjectors)
}

type ConfigInjectorService interface {
	init(addresses []string, log telegraf.Logger) error
	GetConfigs() ([]SharedConfig, error)
	GetTags(address string) (map[string]string, error)
}

// Snmp holds the configuration for the plugin.
type Snmp struct {
	SharedConfig
	Log telegraf.Logger `toml:"-"`

	ConfigInjector `toml:"config_injector"`

	// internal runtime config injection
	injectorService ConfigInjectorService
	injectedConfigs []SharedConfig
}

func (s *Snmp) SetTranslator(name string) {
	s.Translator = name
}

func (*Snmp) SampleConfig() string {
	return sampleConfig
}

func (snmp *Snmp) InitializeInjector() error {
	// Check the injector type and set the corresponding service
	switch snmp.ConfigInjector.Type {
	case "fileInjector":
		snmp.injectorService = &FileConfigInjector{FilePath: snmp.ConfigInjector.FilePath}

	default:
		return fmt.Errorf("unknown config injector type: %s", snmp.ConfigInjector.Type)
	}
	err := snmp.injectorService.init(snmp.Agents, snmp.Log)
	if err != nil {
		return err
	}
	return nil
}

func initializeSnmp(c *SharedConfig, Log telegraf.Logger) error {
	var err error
	switch c.Translator {
	case "gosmi":
		c.translator, err = snmp.NewGosmiTranslator(c.Path, Log)
		if err != nil {
			return err
		}
	case "netsnmp":
		c.translator = snmp.NewNetsnmpTranslator(Log)
	default:
		return errors.New("invalid translator value")
	}

	c.connectionCache = make([]snmp.Connection, len(c.Agents))

	for i := range c.Tables {
		if err := c.Tables[i].Init(c.translator); err != nil {
			return fmt.Errorf("initializing table %s: %w", c.Tables[i].Name, err)
		}
	}

	for i := range c.Fields {
		if err := c.Fields[i].Init(c.translator); err != nil {
			return fmt.Errorf("initializing field %s: %w", c.Fields[i].Name, err)
		}
	}

	if len(c.AgentHostTag) == 0 {
		c.AgentHostTag = "agent_host"
	}
	if c.AgentHostTag != "source" {
		config.PrintOptionValueDeprecationNotice("inputs.snmp", "agent_host_tag", c.AgentHostTag, telegraf.DeprecationInfo{
			Since:  "1.29.0",
			Notice: `set to "source" for consistent usage across plugins or safely ignore this message and continue to use the current value`,
		})
	}

	return nil

}

func (s *Snmp) Init() error {
	if s.ConfigInjector.Type != "" {
		s.Log.Infof("using config injector: [%v] ", s.ConfigInjector.Type)
		if err := s.InitializeInjector(); err != nil {
			return err
		}
		var err error
		s.injectedConfigs, err = s.injectorService.GetConfigs()
		if err != nil {
			return err
		}
		for i := range s.injectedConfigs {
			config := &s.injectedConfigs[i]
			if err := initializeSnmp(config, s.Log); err != nil {
				return err
			}
		}
	} else {

		return initializeSnmp(&s.SharedConfig, s.Log)
	}
	return nil
}

func (s *Snmp) gather(c *SharedConfig, acc telegraf.Accumulator) error {
	var wg sync.WaitGroup
	for i, agent := range c.Agents {
		wg.Add(1)
		go func(i int, agent string) {
			defer wg.Done()
			gs, err := c.getConnection(i)
			if err != nil {
				acc.AddError(fmt.Errorf("agent %s: %w", agent, err))
				return
			}

			// First is the top-level fields. We treat the fields as table prefixes with an empty index.
			t := snmp.Table{
				Name:   c.Name,
				Fields: c.Fields,
			}
			topTags := make(map[string]string)
			cTags := make(map[string]string)
			if err := c.gatherTable(acc, gs, t, topTags, false, cTags); err != nil {
				acc.AddError(fmt.Errorf("agent %s: %w", agent, err))
			}

			if s.injectorService != nil {
				if dt, err := s.injectorService.GetTags(agent); err == nil {
					for key, val := range dt {
						cTags[key] = val
					}
				}
			}
			// Now is the real tables.
			for _, t := range c.Tables {
				if err := c.gatherTable(acc, gs, t, topTags, true, cTags); err != nil {
					acc.AddError(fmt.Errorf("agent %s: gathering table %s: %w", agent, t.Name, err))
				}
			}
		}(i, agent)
	}
	wg.Wait()

	return nil
}

// Gather retrieves all the configured fields and tables.
// Any error encountered does not halt the process. The errors are accumulated
// and returned at the end.
func (s *Snmp) Gather(acc telegraf.Accumulator) error {
	if s.ConfigInjector.Type != "" {
		var wg sync.WaitGroup
		// errChan := make(chan error, len(s.injectedConfigs))

		for _, config := range s.injectedConfigs {
			wg.Add(1)
			go func(cfg SharedConfig) {
				defer wg.Done()
				if err := s.gather(&cfg, acc); err != nil {
					// errChan <- err
				}
			}(config)
		}
		wg.Wait()
		// close(errChan)

	} else {
		return s.gather(&s.SharedConfig, acc)
	}

	return nil
}

func (c *SharedConfig) gatherTable(acc telegraf.Accumulator, gs snmp.Connection, t snmp.Table, topTags map[string]string, walk bool, cTags map[string]string) error {
	rt, err := t.Build(gs, walk)
	if err != nil {
		return err
	}

	for _, tr := range rt.Rows {
		if !walk {
			// top-level table. Add tags to topTags.
			for k, v := range tr.Tags {
				topTags[k] = v
			}
		} else {
			// real table. Inherit any specified tags.
			for _, k := range t.InheritTags {
				if v, ok := topTags[k]; ok {
					tr.Tags[k] = v
				}
			}
		}
		if _, ok := tr.Tags[c.AgentHostTag]; !ok {
			tr.Tags[c.AgentHostTag] = gs.Host()
		}
		// add the device specific tag
		for key, val := range cTags {
			tr.Tags[key] = val
		}
		acc.AddFields(rt.Name, tr.Fields, tr.Tags, rt.Time)
	}

	return nil
}

// getConnection creates a snmpConnection (*gosnmp.GoSNMP) object and caches the
// result using `agentIndex` as the cache key.  This is done to allow multiple
// connections to a single address.  It is an error to use a connection in
// more than one goroutine.
func (c *SharedConfig) getConnection(idx int) (snmp.Connection, error) {
	if gs := c.connectionCache[idx]; gs != nil {
		if err := gs.Reconnect(); err != nil {
			return gs, fmt.Errorf("reconnecting: %w", err)
		}

		return gs, nil
	}

	agent := c.Agents[idx]

	gs, err := snmp.NewWrapper(c.ClientConfig)
	if err != nil {
		return nil, err
	}

	err = gs.SetAgent(agent)
	if err != nil {
		return nil, err
	}

	c.connectionCache[idx] = gs

	if err := gs.Connect(); err != nil {
		return nil, fmt.Errorf("setting up connection: %w", err)
	}

	return gs, nil
}

func init() {
	inputs.Add("snmp", func() telegraf.Input {
		return &Snmp{
			SharedConfig: SharedConfig{
				Name: "snmp",
				ClientConfig: snmp.ClientConfig{
					Retries:        3,
					MaxRepetitions: 10,
					Timeout:        config.Duration(5 * time.Second),
					Version:        2,
					Path:           []string{"/usr/share/snmp/mibs"},
					Community:      "public",
				},
			},
		}
	})
}
