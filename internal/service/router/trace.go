package router

import "github.com/gateyes/gateway/internal/service/provider"

type OrderTrace struct {
	Initial     []string  `json:"initial"`
	Rule        RuleTrace `json:"rule"`
	AfterRule   []string  `json:"after_rule"`
	Ranker      string    `json:"ranker"`
	AfterRanker []string  `json:"after_ranker"`
	Strategy    string    `json:"strategy"`
	Ordered     []string  `json:"ordered"`
}

type RuleTrace struct {
	Matched   bool     `json:"matched"`
	RuleName  string   `json:"rule_name,omitempty"`
	Providers []string `json:"providers,omitempty"`
}

func providerNameList(items []provider.Provider) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	return names
}
