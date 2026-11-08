package growwapi

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"flag"
)

const (
	ConfigSourceDefault ConfigSource = "default"
	ConfigSourceEnv     ConfigSource = "env"
	ConfigSourceFlag    ConfigSource = "flag"
	ConfigSourceFile    ConfigSource = "file"
)

type ConfigSource string

// ConfigField describes a single tracked field: its resolved value, the
// source that contributed it (default/env/flag/file), and the names the
// loader uses to find it.
type ConfigField struct {
	Value  any
	Source ConfigSource
	EnvKey string
	Flag   string
	YAML   string
}

// Config holds runtime configuration for the SDK, CLI, and REST API.
// Each tracked field records the source it was loaded from so the
// user can introspect where a particular value came from.
//
// Precedence (highest first): flag > env > file > default.
type Config struct {
	fields map[string]*ConfigField
	order  []string

	// All public fields below are tracked via struct tags:
	//   env:"FOO"     -> ENV variable
	//   flag:"foo"    -> CLI flag name
	//   yaml:"foo"    -> YAML key
	//   config:"foo"  -> logical config key (used for sources map)
	//   default:"..." -> default value as string
	//   tracked:"-"   -> ignored by the loader

	AccessToken string `env:"GROWW_ACCESS_TOKEN" flag:"access-token" yaml:"access_token" config:"access_token" default:""`

	APIBaseURL string `env:"GROWW_API_BASE_URL" flag:"api-base-url" yaml:"api_base_url" config:"api_base_url" default:"https://api.groww.in"`

	HTTPTimeoutSeconds int `env:"GROWW_HTTP_TIMEOUT" flag:"http-timeout" yaml:"http_timeout" config:"http_timeout" default:"30"`

	// REST API mode config
	RESTListenAddr string `env:"GROWW_REST_ADDR" flag:"rest-addr" yaml:"rest_addr" config:"rest_addr" default:":8080"`

	// Path to a YAML config file (can also be set via GROWW_CONFIG or -config)
	ConfigPath string `env:"GROWW_CONFIG" flag:"config" yaml:"-" config:"config_path" default:"" tracked:"-"`
}

// NewConfig returns a Config populated with defaults. Call Load to layer
// env, file and flag sources on top.
func NewConfig() *Config {
	c := &Config{fields: map[string]*ConfigField{}}
	c.register()
	c.applyDefaults()
	return c
}

// Fields returns the field metadata map keyed by the value of the
// `config:"..."` struct tag.
func (c *Config) Fields() map[string]*ConfigField { return c.fields }

// SourceOf returns where the given field was loaded from. The key is the
// value of the struct tag `config:"..."`.
func (c *Config) SourceOf(key string) (ConfigSource, bool) {
	f, ok := c.fields[key]
	if !ok {
		return "", false
	}
	return f.Source, true
}

// Field returns the metadata for a tracked field.
func (c *Config) Field(key string) (*ConfigField, bool) {
	f, ok := c.fields[key]
	return f, ok
}

// LoadOptions controls how Config.Load resolves values.
type LoadOptions struct {
	Args    []string // override os.Args[1:]
	Env     []string // override os.Environ()
	FS      FileReader
	FlagSet *flag.FlagSet
}

// FileReader abstracts a YAML file on disk so callers can inject tests.
type FileReader func(path string) ([]byte, error)

// Load resolves configuration in the following order (highest wins):
//  1. flags
//  2. environment variables
//  3. file (YAML, path from -config / GROWW_CONFIG)
//  4. defaults
//
// Each tracked field records the source that ultimately provided its value.
func (c *Config) Load(opts LoadOptions) error {
	if opts.Env == nil {
		opts.Env = os.Environ()
	}
	if opts.Args == nil {
		opts.Args = os.Args[1:]
	}
	if opts.FS == nil {
		opts.FS = os.ReadFile
	}
	if opts.FlagSet == nil {
		opts.FlagSet = flag.NewFlagSet("growwapi", flag.ContinueOnError)
		opts.FlagSet.SetOutput(devNull{})
	}
	// Silently ignore unknown flags (e.g. -test.testlogfile from `go test`).
	opts.FlagSet.Usage = func() {}

	c.bindFlags(opts.FlagSet)

	nonFlagArgs, err := parseFlagsAllowingHelp(opts.FlagSet, opts.Args)
	if err != nil {
		return err
	}
	_ = nonFlagArgs

	c.resolveConfigPath(opts.FlagSet, opts.Env)

	if c.ConfigPath != "" {
		if err := c.loadFile(opts.FS); err != nil {
			return err
		}
	}

	c.applyEnv(opts.Env)
	c.applyFlags(opts.FlagSet)

	if err := c.Validate(); err != nil {
		return err
	}
	return nil
}

func (c *Config) resolveConfigPath(fs *flag.FlagSet, env []string) {
	if fs != nil {
		if f := fs.Lookup("config"); f != nil && f.Value.String() != "" {
			c.ConfigPath = f.Value.String()
			return
		}
	}
	if p := getEnv(env, "GROWW_CONFIG"); p != "" {
		c.ConfigPath = p
	}
}

func (c *Config) loadFile(fs FileReader) error {
	raw, err := fs(c.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// If the user explicitly set GROWW_CONFIG or -config they expect it to exist.
			if os.Getenv("GROWW_CONFIG") != "" || c.ConfigPath != "" {
				if c.ConfigPath != "" {
					return fmt.Errorf("config file %q: %w", c.ConfigPath, err)
				}
			}
			return nil
		}
		return fmt.Errorf("read config file %q: %w", c.ConfigPath, err)
	}
	root, err := parseYAMLMap(raw)
	if err != nil {
		return fmt.Errorf("parse config file %q: %w", c.ConfigPath, err)
	}
	for _, key := range c.order {
		cf := c.fields[key]
		if cf.YAML == "" || cf.YAML == "-" {
			continue
		}
		if v, ok := root[cf.YAML]; ok {
			c.setField(key, v, ConfigSourceFile)
		}
	}
	return nil
}

func parseFlagsAllowingHelp(fs *flag.FlagSet, args []string) ([]string, error) {
	// -h / --help handled by stdlib flag via ContinueOnError
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, err
		}
		return nil, fmt.Errorf("parse flags: %w", err)
	}
	return fs.Args(), nil
}

// Validate ensures required fields are present.
func (c *Config) Validate() error {
	if c.AccessToken == "" {
		return errors.New("GROWW_ACCESS_TOKEN (or -access-token / access_token in YAML) is required")
	}
	if c.APIBaseURL == "" {
		return errors.New("api_base_url is required and cannot be empty")
	}
	if c.HTTPTimeoutSeconds <= 0 {
		c.HTTPTimeoutSeconds = 30
	}
	return nil
}

// Snapshot returns the resolved config as a plain map (no source metadata).
func (c *Config) Snapshot() map[string]any {
	out := make(map[string]any, len(c.fields))
	for k, f := range c.fields {
		out[k] = f.Value
	}
	return out
}

// Sources returns a snapshot of which source each field came from.
func (c *Config) Sources() map[string]ConfigSource {
	out := make(map[string]ConfigSource, len(c.fields))
	for k, f := range c.fields {
		out[k] = f.Source
	}
	return out
}

func (c *Config) register() {
	v := reflect.ValueOf(c).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		if f.Tag.Get("tracked") == "-" {
			continue
		}
		key := f.Tag.Get("config")
		if key == "" {
			key = strings.ToLower(f.Name)
		}
		c.fields[key] = &ConfigField{
			EnvKey: f.Tag.Get("env"),
			Flag:   f.Tag.Get("flag"),
			YAML:   f.Tag.Get("yaml"),
			Source: ConfigSourceDefault,
		}
		c.order = append(c.order, key)
	}
}

func (c *Config) applyDefaults() {
	t := reflect.TypeOf(c).Elem()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Tag.Get("tracked") == "-" {
			continue
		}
		def := f.Tag.Get("default")
		key := f.Tag.Get("config")
		if key == "" {
			key = strings.ToLower(f.Name)
		}
		c.setField(key, def, ConfigSourceDefault)
	}
}

func (c *Config) bindFlags(fs *flag.FlagSet) {
	// -config is always available so the loader can locate the YAML file.
	if fs.Lookup("config") == nil {
		fs.StringVar(&c.ConfigPath, "config", c.ConfigPath, "config file path (env: GROWW_CONFIG)")
	}
	for _, key := range c.order {
		cf := c.fields[key]
		if cf.Flag == "" {
			continue
		}
		v := reflect.ValueOf(c).Elem().FieldByName(c.structFieldName(key))
		if !v.CanAddr() {
			continue
		}
		switch v.Kind() {
		case reflect.String:
			fv := &cachedString{setInto: v}
			fv.current = v.String()
			fs.Var(fv, cf.Flag, "")
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fv := &cachedInt{setInto: v}
			fv.current = v.Int()
			fs.Var(fv, cf.Flag, "")
		case reflect.Bool:
			fv := &cachedBool{setInto: v}
			fv.current = v.Bool()
			fs.Var(fv, cf.Flag, "")
		}
	}
}

// cached* flag.Value implementations write through to the struct field but
// return the value captured at Parse time so later env/file overrides don't
// clobber the precedence calculation in applyFlags.

type cachedString struct {
	setInto reflect.Value
	current string
}

func (c *cachedString) String() string { return c.current }
func (c *cachedString) Set(s string) error {
	c.current = s
	c.setInto.SetString(s)
	return nil
}

type cachedInt struct {
	setInto reflect.Value
	current int64
}

func (c *cachedInt) String() string { return strconv.FormatInt(c.current, 10) }
func (c *cachedInt) Set(s string) error {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	c.current = n
	c.setInto.SetInt(n)
	return nil
}

type cachedBool struct {
	setInto reflect.Value
	current bool
}

func (c *cachedBool) String() string { return strconv.FormatBool(c.current) }
func (c *cachedBool) Set(s string) error {
	b, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	c.current = b
	c.setInto.SetBool(b)
	return nil
}

func (c *Config) structFieldName(configKey string) string {
	v := reflect.ValueOf(c).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Tag.Get("tracked") == "-" {
			continue
		}
		if f.Tag.Get("config") == configKey {
			return f.Name
		}
	}
	return ""
}

func (c *Config) applyEnv(env []string) {
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name := kv[:eq]
		val := kv[eq+1:]
		for _, key := range c.order {
			cf := c.fields[key]
			if cf.EnvKey == "" {
				continue
			}
			if !strings.EqualFold(cf.EnvKey, name) {
				continue
			}
			c.setField(key, val, ConfigSourceEnv)
		}
	}
}

func (c *Config) applyFlags(fs *flag.FlagSet) {
	fs.Visit(func(f *flag.Flag) {
		for _, key := range c.order {
			cf := c.fields[key]
			if cf.Flag != f.Name {
				continue
			}
			c.setField(key, f.Value.String(), ConfigSourceFlag)
			return
		}
	})
}

func (c *Config) setField(key, raw string, src ConfigSource) {
	cf, ok := c.fields[key]
	if !ok {
		return
	}

	name := c.structFieldName(key)
	rv := reflect.ValueOf(c).Elem().FieldByName(name)
	switch rv.Kind() {
	case reflect.String:
		rv.SetString(raw)
		cf.Value = rv.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return
		}
		rv.SetInt(n)
		cf.Value = rv.Int()
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return
		}
		rv.SetBool(b)
		cf.Value = rv.Bool()
	default:
		cf.Value = raw
	}
	cf.Source = src
}

func getEnv(env []string, key string) string {
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		if strings.EqualFold(kv[:eq], key) {
			return kv[eq+1:]
		}
	}
	return ""
}

type devNull struct{}

func (devNull) Write(p []byte) (int, error) { return len(p), nil }
