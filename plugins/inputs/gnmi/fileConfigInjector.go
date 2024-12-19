package gnmi

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/influxdata/telegraf"
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
	DeviceID         string            `json:"device_id"`
	Address          string            `json:"address"`
	Subscriptions    []string          `json:"subscriptions"`
	TagSubscriptions []string          `json:"tag_subscriptions"`
	SharedConfig     string            `json:"common_config"`
	SharedTagGroups  []string          `json:"shared_tag_group"`
	Tags             map[string]string `json:"tags"`
}

type SharedTags struct {
	ID   string            `json:"id"`   // Unique identifier for shared tags
	Tags map[string]string `json:"tags"` // Shared tags
}

type Subscription struct {
	Id string `json:"id"`
	subscription
}

type TagSubscription struct {
	ID string `json:"id"`
	tagSubscription
}

type InputData struct {
	SharedSubscriptions struct {
		Subscriptions    map[string][]Subscription    `json:"subscriptions"`     // map by ID
		TagSubscriptions map[string][]TagSubscription `json:"tag_subscriptions"` // map by ID
	} `json:"shared_subscriptions"`

	SharedCommonConfigs map[string]SharedConfig `json:"shared_common_configs"` // map by ID

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

func (f *FileConfigInjector) groupDevices(d *InputData) ([]SharedConfig, map[string]*DeviceTag) {
	groupMap := make(map[string]*SharedConfig)
	tagMap := make(map[string]*DeviceTag)

	// Iterate through devices to group them
	for _, device := range d.Devices {
		// Create a key for grouping by subscriptions, common_config, and tag_subscriptions
		key := fmt.Sprintf("%v|%v|%v", device.Subscriptions, device.SharedConfig, device.TagSubscriptions)

		// If the group doesn't exist, create a new group entry
		if _, exists := groupMap[key]; !exists {
			thisConfig := d.SharedCommonConfigs[device.SharedConfig]

			for _, key := range device.Subscriptions {
				if sharedSubs, exists := d.SharedSubscriptions.Subscriptions[key]; exists {
					for _, sharedSub := range sharedSubs {
						thisConfig.Subscriptions = append(thisConfig.Subscriptions, sharedSub.subscription)
					}
				} else {
					f.Log.Warnf("Subscription key %s not found in shared subscriptions", key)
				}
			}

			// Add tag subscriptions from device.TagSubscriptions
			for _, key := range device.TagSubscriptions {
				if sharedTagSubs, exists := d.SharedSubscriptions.TagSubscriptions[key]; exists {
					for _, sharedTagSub := range sharedTagSubs {
						// Append only the subscription part
						thisConfig.TagSubscriptions = append(thisConfig.TagSubscriptions, sharedTagSub.tagSubscription)
					}
				} else {
					f.Log.Warnf("Tag subscription key %s not found in shared tag subscriptions", key)
				}
			}

			groupMap[key] = &thisConfig
		}

		tagMap[device.Address] = &DeviceTag{
			sharedTagIds: device.SharedTagGroups,
			tags:         device.Tags,
		}

		// Add device address to the group
		groupMap[key].Addresses = append(groupMap[key].Addresses, device.Address)

	}

	var groups []SharedConfig
	for _, group := range groupMap {
		groups = append(groups, *group)
	}

	return groups, tagMap
}

// GetConfigs reads configuration data from a file and returns a slice of sharedConfig
func (f *FileConfigInjector) GetConfigs(addresses []string) ([]SharedConfig, error) {
	if f.collectorConfigs == nil {
		return nil, fmt.Errorf("gnmi collector configs are not initialized")
	}

	return f.collectorConfigs, nil
}

func (f *FileConfigInjector) init(log telegraf.Logger) error {

	// Simulate loading configs from a file (you can replace this with actual file reading logic)
	fmt.Println("Loading config from file:", f.FilePath)
	var c InputData
	if err := loadJSONFromFile(f.FilePath, &c); err != nil {
		return err
	}
	f.Log = log
	f.sharedTags = c.SharedTags
	groups, tg := f.groupDevices(&c)
	f.deviceTags = tg
	f.collectorConfigs = groups

	fmt.Printf("config: %v", f.deviceTags)
	fmt.Printf("config: %v", groups)

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
