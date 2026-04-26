// Package config loads mole forwarding rules from a YAML file.
// It uses no external dependencies — only the Go standard library.
//
// Supported schema (YAML):
//
//	rules:
//	  - proto: tcp
//	    from: "8080"
//	    to: "localhost:8080"
//	  - proto: unix-server
//	    from: "9090"
//	    to: /tmp/amdinfer.sock
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Rule describes a single forwarding rule.
type Rule struct {
	Proto string // tcp | udp | unix-server | unix-proxy
	From  string // local port number, or Unix socket path for unix-proxy
	To    string // remote host:port, or Unix socket path for unix-server
}

// Load reads a YAML config file and returns the list of rules.
func Load(path string) ([]Rule, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var rules []Rule
	var cur *Rule

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip blank lines and comments.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Top-level "rules:" key — ignore.
		if trimmed == "rules:" {
			continue
		}
		// New list item.
		if strings.HasPrefix(trimmed, "- ") {
			if cur != nil {
				if err := validateRule(cur); err != nil {
					return nil, err
				}
				rules = append(rules, *cur)
			}
			cur = &Rule{}
			trimmed = strings.TrimPrefix(trimmed, "- ")
		}

		// Key: value pair.
		key, val, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Strip optional surrounding quotes.
		val = strings.Trim(val, `"'`)

		if cur == nil {
			cur = &Rule{}
		}
		switch key {
		case "proto":
			cur.Proto = strings.ToLower(val)
		case "from":
			cur.From = val
		case "to":
			cur.To = val
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// Flush last rule.
	if cur != nil {
		if err := validateRule(cur); err != nil {
			return nil, err
		}
		rules = append(rules, *cur)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("no rules found in %s", path)
	}
	return rules, nil
}

func validateRule(r *Rule) error {
	if r.Proto == "" {
		return fmt.Errorf("rule missing proto: %+v", r)
	}
	if r.From == "" {
		return fmt.Errorf("rule missing from: %+v", r)
	}
	if r.To == "" {
		return fmt.Errorf("rule missing to: %+v", r)
	}
	return nil
}
