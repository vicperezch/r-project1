// Package config loads the MCP servers file. The format matches the
// mcpServers block of the Claude Desktop config on purpose, so a server
// configuration published by someone else can be pasted in unchanged.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	TypeStdio = "stdio"
	TypeHTTP  = "http"
)

type Server struct {
	// Type is optional. With no type, a command means stdio and a url means http.
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Disabled keeps an entry in the file without connecting to it.
	Disabled bool `json:"disabled,omitempty"`
}

type File struct {
	Servers map[string]Server `json:"mcpServers"`
}

func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read servers config: %w", err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(f.Servers) == 0 {
		return nil, fmt.Errorf("%s has no mcpServers entries", path)
	}
	for name, s := range f.Servers {
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("server %q: %w", name, err)
		}
	}
	return &f, nil
}

// Names returns the configured server names in a stable order, so tool
// registration and collision suffixes do not depend on map iteration.
func (f *File) Names() []string {
	names := make([]string, 0, len(f.Servers))
	for n := range f.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ResolvedType infers the transport when the file does not state one.
func (s Server) ResolvedType() string {
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case TypeStdio:
		return TypeStdio
	case TypeHTTP, "streamable-http", "streamablehttp", "http-stream":
		return TypeHTTP
	case "":
		if s.Command != "" {
			return TypeStdio
		}
		if s.URL != "" {
			return TypeHTTP
		}
		return ""
	default:
		return strings.ToLower(strings.TrimSpace(s.Type))
	}
}

func (s Server) Validate() error {
	switch s.ResolvedType() {
	case TypeStdio:
		if s.Command == "" {
			return fmt.Errorf("stdio servers need a command")
		}
	case TypeHTTP:
		if s.URL == "" {
			return fmt.Errorf("http servers need a url")
		}
	case "":
		return fmt.Errorf("needs either a command for stdio or a url for http")
	default:
		return fmt.Errorf("unsupported type %q, this client speaks stdio and http", s.Type)
	}
	return nil
}

// Describe is the one line shown by the /servers command.
func (s Server) Describe() string {
	if s.ResolvedType() == TypeHTTP {
		return "http " + s.URL
	}
	parts := append([]string{s.Command}, s.Args...)
	return "stdio " + strings.Join(parts, " ")
}
