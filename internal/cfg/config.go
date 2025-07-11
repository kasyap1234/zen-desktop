package cfg

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	// ConfigDir is the path to the directory storing the application configuration.
	ConfigDir string
	// DataDir is the path to the directory storing the application data.
	DataDir string
	// Version is the current version of the application. Set at compile time for production builds using ldflags (see tasks in the /tasks/build directory).
	Version = "development"
)

//go:embed default-config.json
var defaultConfig embed.FS

type FilterListType string

const (
	FilterListTypeCustom FilterListType = "custom"
)

type UpdatePolicyType string

const (
	UpdatePolicyAutomatic UpdatePolicyType = "automatic"
	UpdatePolicyPrompt    UpdatePolicyType = "prompt"
	UpdatePolicyDisabled  UpdatePolicyType = "disabled"
)

var UpdatePolicyEnum = []struct {
	Value  UpdatePolicyType
	TSName string
}{
	{UpdatePolicyAutomatic, "AUTOMATIC"},
	{UpdatePolicyPrompt, "PROMPT"},
	{UpdatePolicyDisabled, "DISABLED"},
}

// Config stores and manages the configuration for the application.
// Although all fields are public, this is only for use by the JSON marshaller.
// All access to the Config should be done through the exported methods.
type Config struct {
	sync.RWMutex

	Filter struct {
		FilterLists []FilterList `json:"filterLists"`
		MyRules     []string     `json:"myRules"`
	} `json:"filter"`
	Certmanager struct {
		CAInstalled bool `json:"caInstalled"`
	} `json:"certmanager"`
	Proxy struct {
		Port         int      `json:"port"`
		IgnoredHosts []string `json:"ignoredHosts"`
		PACPort      int      `json:"pacPort"`
	} `json:"proxy"`
	UpdatePolicy UpdatePolicyType `json:"updatePolicy"`

	Locale string `json:"locale"`

	// firstLaunch is true if the application is being run for the first time.
	firstLaunch bool
}

type FilterList struct {
	Name    string         `json:"name"`
	Type    FilterListType `json:"type"`
	URL     string         `json:"url"`
	Enabled bool           `json:"enabled"`
	Trusted bool           `json:"trusted"`
}

type DebugData struct {
	EnabledFilterListURLs []string `json:"enabledFilterListURLs"`
	CustomRules           []string `json:"customRules"`
	Platform              string   `json:"platform"`
	Architecture          string   `json:"architecture"`
	Version               string   `json:"version"`
}

func (c *Config) ExportDebugData() (string, error) {
	c.RLock()
	defer c.RUnlock()
	var enabledFilterListURLs []string
	for _, filterList := range c.Filter.FilterLists {
		if filterList.Enabled {
			enabledFilterListURLs = append(enabledFilterListURLs, filterList.URL)
		}
	}
	debugData := DebugData{
		EnabledFilterListURLs: enabledFilterListURLs,
		CustomRules:           c.Filter.MyRules,
		Platform:              runtime.GOOS,
		Architecture:          runtime.GOARCH,
		Version:               Version,
	}
	jsonData, err := json.MarshalIndent(debugData, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal debug data: %w", err)
	}
	return string(jsonData), nil
}

func (f *FilterList) UnmarshalJSON(data []byte) error {
	type TempFilterList FilterList
	var temp TempFilterList

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	if temp.Name == "" {
		return errors.New("name is required")
	}

	if temp.URL == "" {
		return errors.New("URL is required")
	}

	if temp.Type == "" {
		return errors.New("type is required")
	}

	*f = FilterList(temp)
	return nil
}

func init() {
	var err error
	ConfigDir, err = getConfigDir()
	if err != nil {
		log.Fatalf("failed to get config dir: %v", err)
	}
	stat, err := os.Stat(ConfigDir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(ConfigDir, 0755); err != nil {
			log.Fatalf("failed to create config dir: %v", err)
		}
		stat, err = os.Stat(ConfigDir)
	}
	if err != nil {
		log.Fatalf("failed to stat config dir: %v", err)
	}
	if !stat.IsDir() {
		log.Fatalf("config dir is not a directory: %s", ConfigDir)
	}

	DataDir, err = getDataDir()
	if err != nil {
		log.Fatalf("failed to get data dir: %v", err)
	}
	stat, err = os.Stat(DataDir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(DataDir, 0755); err != nil {
			log.Fatalf("failed to create data dir: %v", err)
		}
		stat, err = os.Stat(DataDir)
	}
	if err != nil {
		log.Fatalf("failed to stat data dir: %v", err)
	}
	if !stat.IsDir() {
		log.Fatalf("data dir is not a directory: %s", DataDir)
	}
}

func NewConfig() (*Config, error) {
	c := &Config{}

	configFile := filepath.Join(ConfigDir, "config.json")
	var configData []byte
	if _, err := os.Stat(configFile); !os.IsNotExist(err) {
		configData, err = os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %v", err)
		}
	} else {
		configData, err = defaultConfig.ReadFile("default-config.json")
		if err != nil {
			return nil, fmt.Errorf("failed to read default config file: %v", err)
		}
		if err := os.WriteFile(configFile, configData, 0644); err != nil {
			return nil, fmt.Errorf("failed to write config file: %v", err)
		}
		c.firstLaunch = true
	}

	if err := json.Unmarshal(configData, c); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	return c, nil
}

// Save saves the config to disk.
// It is not thread-safe, and should only be called if the caller has
// a lock on the config.
func (c *Config) Save() error {
	configData, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	configFile := filepath.Join(ConfigDir, "config.json")
	err = os.WriteFile(configFile, configData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// GetFilterLists returns the list of enabled filter lists.
func (c *Config) GetFilterLists() []FilterList {
	c.RLock()
	defer c.RUnlock()

	return c.Filter.FilterLists
}

// AddFilterList adds a new filter list to the list of enabled filter lists.
func (c *Config) AddFilterList(list FilterList) string {
	c.Lock()
	defer c.Unlock()

	for _, existingList := range c.Filter.FilterLists {
		if existingList.URL == list.URL {
			return fmt.Sprintf("filter list with the URL '%s' already exists", list.URL)
		}
	}

	c.Filter.FilterLists = append(c.Filter.FilterLists, list)
	if err := c.Save(); err != nil {
		log.Printf("failed to save config: %v", err)
		return err.Error()
	}
	return ""
}

func (c *Config) AddFilterLists(lists []FilterList) error {
	c.Lock()
	defer c.Unlock()

	c.Filter.FilterLists = append(c.Filter.FilterLists, lists...)
	if err := c.Save(); err != nil {
		log.Printf("failed to save config: %v", err)
		return err
	}
	return nil
}

// RemoveFilterList removes a filter list from the list of enabled filter lists.
func (c *Config) RemoveFilterList(url string) string {
	c.Lock()
	defer c.Unlock()

	for i, filterList := range c.Filter.FilterLists {
		if filterList.URL == url {
			c.Filter.FilterLists = append(c.Filter.FilterLists[:i], c.Filter.FilterLists[i+1:]...)
			break
		}
	}
	if err := c.Save(); err != nil {
		log.Printf("failed to save config: %v", err)
		return err.Error()
	}
	return ""
}

// ToggleFilterList toggles the enabled state of a filter list.
func (c *Config) ToggleFilterList(url string, enabled bool) string {
	c.Lock()
	defer c.Unlock()

	for i, filterList := range c.Filter.FilterLists {
		if filterList.URL == url {
			c.Filter.FilterLists[i].Enabled = enabled
			break
		}
	}
	if err := c.Save(); err != nil {
		log.Printf("failed to save config: %v", err)
		return err.Error()
	}
	return ""
}

// GetTargetTypeFilterLists returns the list of filter lists with particular type.
func (c *Config) GetTargetTypeFilterLists(targetType FilterListType) []FilterList {
	c.RLock()
	defer c.RUnlock()

	var filterLists []FilterList
	for _, filterList := range c.Filter.FilterLists {
		if filterList.Type == targetType {
			filterLists = append(filterLists, filterList)
		}
	}
	return filterLists
}

func (c *Config) GetMyRules() []string {
	c.RLock()
	defer c.RUnlock()

	return c.Filter.MyRules
}

func (c *Config) SetMyRules(rules []string) error {
	c.Lock()
	defer c.Unlock()

	c.Filter.MyRules = rules
	if err := c.Save(); err != nil {
		err = fmt.Errorf("failed to save config: %v", err)
		log.Println(err)
		return err
	}
	return nil
}

// GetPort returns the port the proxy is set to listen on.
func (c *Config) GetPort() int {
	c.RLock()
	defer c.RUnlock()

	return c.Proxy.Port
}

// SetPort sets the port the proxy is set to listen on.
func (c *Config) SetPort(port int) string {
	c.Lock()
	defer c.Unlock()

	c.Proxy.Port = port
	if err := c.Save(); err != nil {
		log.Printf("failed to save config: %v", err)
		return err.Error()
	}
	return ""
}

// GetIgnoredHosts returns the list of ignored hosts.
func (c *Config) GetIgnoredHosts() []string {
	c.RLock()
	defer c.RUnlock()

	return c.Proxy.IgnoredHosts
}

// SetIgnoredHosts sets the list of ignored hosts.
func (c *Config) SetIgnoredHosts(hosts []string) error {
	c.Lock()
	defer c.Unlock()

	c.Proxy.IgnoredHosts = hosts
	if err := c.Save(); err != nil {
		log.Printf("failed to save config: %v", err)
		return err
	}
	return nil
}

// GetCAInstalled returns whether the CA is installed.
func (c *Config) GetCAInstalled() bool {
	c.RLock()
	defer c.RUnlock()

	return c.Certmanager.CAInstalled
}

// SetCAInstalled sets whether the CA is installed.
func (c *Config) SetCAInstalled(caInstalled bool) {
	c.Lock()
	defer c.Unlock()

	c.Certmanager.CAInstalled = caInstalled
	if err := c.Save(); err != nil {
		log.Printf("failed to save config: %v", err)
	}
}

func (c *Config) GetPACPort() int {
	c.RLock()
	defer c.RUnlock()

	return c.Proxy.PACPort
}

func (c *Config) GetVersion() string {
	return Version
}

func (c *Config) GetUpdatePolicy() UpdatePolicyType {
	c.RLock()
	defer c.RUnlock()

	return c.UpdatePolicy
}

func (c *Config) SetUpdatePolicy(p UpdatePolicyType) {
	c.Lock()
	defer c.Unlock()

	c.UpdatePolicy = p
	if err := c.Save(); err != nil {
		log.Printf("failed to save config: %v", err)
	}
}

func (c *Config) GetLocale() string {
	c.RLock()
	defer c.RUnlock()

	return c.Locale
}

func (c *Config) SetLocale(l string) {
	c.Lock()
	defer c.Unlock()

	c.Locale = l
	if err := c.Save(); err != nil {
		log.Printf("failed to save config: %v", err)
	}
}
