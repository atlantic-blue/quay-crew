// Package messaging is the event log boundary: workspace namespaced topic names and a thin Kafka
// client used to publish records to the log and consume them as a group. Locally the log is
// Redpanda; in the cloud it is managed Kafka or Redpanda. Only this package knows about the broker.
package messaging

import (
	"fmt"
	"strings"
)

// Topic returns the Kafka topic name for a logical stream within a workspace, namespaced so workspaces
// stay isolated on one cluster. For example Topic("acme", "inbound") returns "acme.inbound".
func Topic(workspace, stream string) (string, error) {
	if err := validateName("workspace", workspace); err != nil {
		return "", err
	}
	if err := validateName("stream", stream); err != nil {
		return "", err
	}
	return workspace + "." + stream, nil
}

func validateName(kind, value string) error {
	if value == "" {
		return fmt.Errorf("messaging: %s must not be empty", kind)
	}
	if strings.ContainsAny(value, ". /\t\n") {
		return fmt.Errorf("messaging: %s %q must not contain a dot, slash, or whitespace", kind, value)
	}
	return nil
}
