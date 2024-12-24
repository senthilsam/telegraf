package snmp

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal/snmp"
)

// FileConfigInjector implements the ConfigInjector interface for file-based config loading
type FileConfigInjector struct {
	FilePath string

	Log telegraf.Logger

	sharedTags       map[string]map[string]string
	deviceTags       map[string]*DeviceTag
	collectorConfigs []SharedConfig
}

type DeviceTag struct {
	sharedTagIds []string
	tags         map[string]string
}

type Device struct {
	DeviceID        string            `json:"device_id"`
	Target          string            `json:"address"`
	Fields          []string          `json:"fields"`
	Tables          []string          `json:"tables"`
	SharedConfig    string            `json:"common_config"`
	SharedTagGroups []string          `json:"shared_tag_group"`
	Tags            map[string]string `json:"tags"`
}

type SharedTags struct {
	ID   string            `json:"id"`   // Unique identifier for shared tags
	Tags map[string]string `json:"tags"` // Shared tags
}

type JTable struct {
	Id          string   `json:"id"`
	Name        string   `json:"name"`
	InheritTags []string `json:"inherit_tags"`
	IndexAsTag  bool     `json:"index_as_tag"`
	Oid         string   `json:"oid"`
	Fields      []string `json:"fields"`
}

type Field struct {
	ID string `json:"id"`
	snmp.Field
}

type InputData struct {
	SharedTables map[string][]JTable `json:"shared_tables"` // map by ID
	SharedFields map[string][]Field  `json:"shared_fields"` // map by ID

	SharedCommonConfigs map[string]SharedConfig `json:"shared_configs"` // map by ID

	SharedTags map[string]map[string]string `json:"shared_tags"` // map by group ID

	Devices []Device `json:"devices"`
}

func loadJSONFromFile(filePath string, i *InputData) error {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("could not open file: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&i); err != nil {
		return fmt.Errorf("could not decode JSON: %w", err)
	}

	return nil
}

func (f *FileConfigInjector) groupDevices(d *InputData, targets []string) ([]SharedConfig, map[string]*DeviceTag) {
	groupMap := make(map[string]*SharedConfig)
	tagMap := make(map[string]*DeviceTag)

	targetsMap := make(map[string]bool)
	for _, target := range targets {
		targetsMap[target] = true
	}

	for _, device := range d.Devices {

		if _, exists := targetsMap[device.Target]; !exists {
			// not my current target
			continue
		}
		// Create a key for grouping by subscriptions, common_config, and tag_subscriptions
		key := fmt.Sprintf("%v|%v|%v", device.Fields, device.SharedConfig, device.Tables)

		// create if needed
		if _, exists := groupMap[key]; !exists {
			thisConfig := d.SharedCommonConfigs[device.SharedConfig]

			// add fields if available
			for _, key := range device.Fields {
				if sharedFields, exists := d.SharedFields[key]; exists {
					for _, fields := range sharedFields {
						thisConfig.Fields = append(thisConfig.Fields, fields.Field)
					}
				} else {
					f.Log.Warnf("Filed key %s not found in shared Fileds", key)
				}
			}

			// Add tables
			for _, key := range device.Tables {
				if sharedTables, exists := d.SharedTables[key]; exists {
					for _, sharedTable := range sharedTables {
						thisTable := snmp.Table{
							Name:        sharedTable.Name,
							InheritTags: sharedTable.InheritTags,
							Oid:         sharedTable.Oid,
							IndexAsTag:  sharedTable.IndexAsTag,
						}
						for _, field := range sharedTable.Fields {
							if sharedFields, exists := d.SharedFields[field]; exists {
								for _, fields := range sharedFields {
									thisTable.Fields = append(thisTable.Fields, fields.Field)
								}
							} else {
								f.Log.Warnf("Filed key %s not found in shared Fileds for table %s", key, sharedTable.Id)
							}
						}

						thisConfig.Tables = append(thisConfig.Tables, thisTable)
					}
				} else {
					f.Log.Warnf("Tables key %s not found in shared tables", key)
				}
			}

			groupMap[key] = &thisConfig
		}

		tagMap[device.Target] = &DeviceTag{
			sharedTagIds: device.SharedTagGroups,
			tags:         device.Tags,
		}

		// Add device address to the group
		groupMap[key].Agents = append(groupMap[key].Agents, device.Target)

	}

	var groups []SharedConfig
	counter := 1
	for _, group := range groupMap {

		f.Log.Infof("group%d Targets count: %d", counter, len(group.Agents))
		groups = append(groups, *group)
		counter++
	}
	f.Log.Info("Total grouped configs: ", counter-1)
	return groups, tagMap
}

// GetConfigs reads configuration data from a file and returns a slice of sharedConfig
func (f *FileConfigInjector) GetConfigs() ([]SharedConfig, error) {
	if f.collectorConfigs == nil {
		return nil, fmt.Errorf("gnmi collector configs are not initialized")
	}

	return f.collectorConfigs, nil
}

func (f *FileConfigInjector) init(addresses []string, log telegraf.Logger) error {

	// Simulate loading configs from a file (you can replace this with actual file reading logic)
	fmt.Println("Loading config from file:", f.FilePath)
	var c InputData
	if err := loadJSONFromFile(f.FilePath, &c); err != nil {
		return err
	}
	f.Log = log
	f.sharedTags = c.SharedTags
	groups, tg := f.groupDevices(&c, addresses)
	f.deviceTags = tg
	f.collectorConfigs = groups

	// fmt.Printf("config: %v", f.deviceTags)
	// fmt.Printf("config: %v", groups)

	return nil
}

func (f *FileConfigInjector) GetTags(address string) (map[string]string, error) {

	t := make(map[string]string)
	// Check if the address exists in f.deviceTags
	deviceTags, exists := f.deviceTags[address]
	if !exists {
		// If the address doesn't exist, return an empty map
		return t, nil
	}

	// Add the tags from f.deviceTags for this address
	for key, value := range deviceTags.tags {
		t[key] = value
	}

	// Extend the tags map with the shared tags from f.sharedTags
	for _, sharedTagID := range f.deviceTags[address].sharedTagIds {
		// Check if the sharedTagID exists in f.sharedTags
		if sharedTag, sharedExists := f.sharedTags[sharedTagID]; sharedExists {
			// Add the shared tag's entries to the map
			for key, value := range sharedTag {
				t[key] = value
			}
		}
	}

	// Return the map with the tags (from both deviceTags and sharedTags)
	return t, nil

}
