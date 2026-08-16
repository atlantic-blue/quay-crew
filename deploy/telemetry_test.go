package deploy

import (
	"net"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The telemetry stack is four containers that only work when they agree with each other about names
// and ports, and none of that agreement is visible in any one file. A wrong host is not a parse
// error: the collector starts, the dashboard stays empty, and nothing on screen says why. These
// tests hold the halves together.

type collectorConfig struct {
	Exporters map[string]struct {
		Endpoint string `yaml:"endpoint"`
	} `yaml:"exporters"`
	Service struct {
		Pipelines map[string]struct {
			Exporters []string `yaml:"exporters"`
		} `yaml:"pipelines"`
	} `yaml:"service"`
}

type prometheusConfig struct {
	ScrapeConfigs []struct {
		JobName       string `yaml:"job_name"`
		StaticConfigs []struct {
			Targets []string `yaml:"targets"`
		} `yaml:"static_configs"`
	} `yaml:"scrape_configs"`
}

type grafanaConfig struct {
	Datasources []struct {
		Name string `yaml:"name"`
		URL  string `yaml:"url"`
	} `yaml:"datasources"`
}

func readYAML[T any](t *testing.T, path string) T {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var parsed T
	if err := yaml.Unmarshal(contents, &parsed); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return parsed
}

// composeServices is every service the compose file defines, which is every name that resolves
// inside the compose network.
func composeServices(t *testing.T) map[string]bool {
	t.Helper()
	var compose struct {
		Services map[string]yaml.Node `yaml:"services"`
	}
	contents, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("reading the compose file: %v", err)
	}
	if err := yaml.Unmarshal(contents, &compose); err != nil {
		t.Fatalf("parsing the compose file: %v", err)
	}
	if len(compose.Services) == 0 {
		t.Fatal("no services found in the compose file, so this test proves nothing")
	}
	names := make(map[string]bool, len(compose.Services))
	for name := range compose.Services {
		names[name] = true
	}
	return names
}

// TestEveryExporterAPipelineNamesIsDefined catches the typo that stops the collector booting at all.
func TestEveryExporterAPipelineNamesIsDefined(t *testing.T) {
	config := readYAML[collectorConfig](t, "otel-collector.yaml")
	if len(config.Service.Pipelines) == 0 {
		t.Fatal("the collector defines no pipelines, so this test proves nothing")
	}

	var checked int
	for pipeline, spec := range config.Service.Pipelines {
		if len(spec.Exporters) == 0 {
			t.Errorf("the %s pipeline names no exporter, so everything it receives is discarded", pipeline)
		}
		for _, exporter := range spec.Exporters {
			checked++
			if _, defined := config.Exporters[exporter]; !defined {
				t.Errorf("the %s pipeline exports to %q and no exporter by that name is defined, so the collector refuses to start",
					pipeline, exporter)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no pipeline names an exporter, which cannot be right")
	}
}

// TestEveryPipelineReachesSomethingThatKeepsIt is the one that would have caught the state this
// stack sat in: three pipelines whose only exporter was debug, which prints a summary and drops
// what it was given. Every signal looked wired and none of it was stored anywhere.
func TestEveryPipelineReachesSomethingThatKeepsIt(t *testing.T) {
	config := readYAML[collectorConfig](t, "otel-collector.yaml")

	// The logs pipeline is knowingly still debug only: the services log to their own stdout and
	// nothing forwards it. Naming it here rather than skipping the check keeps the exception
	// deliberate, so the day it is wired this test asks to be updated.
	discardOnly := map[string]string{
		"logs": "logs are structured JSON on each service's own stdout and nothing forwards them yet, see issue 12",
	}

	for pipeline, spec := range config.Service.Pipelines {
		var keeps bool
		for _, exporter := range spec.Exporters {
			if exporter != "debug" {
				keeps = true
			}
		}
		if keeps {
			continue
		}
		if _, known := discardOnly[pipeline]; known {
			continue
		}
		t.Errorf("the %s pipeline exports to debug only, so what it receives is summarised into the collector's log and dropped",
			pipeline)
	}
}

// TestEveryHostTheStackNamesIsAServiceInIt sweeps the three files that name another container by
// host. A wrong name here fails at run time and looks like an empty dashboard.
func TestEveryHostTheStackNamesIsAServiceInIt(t *testing.T) {
	services := composeServices(t)
	var checked int

	check := func(where, host string) {
		checked++
		// A service is free to bind its own listening socket to every interface, and to talk to
		// itself by loopback. Neither names another container.
		if host == "0.0.0.0" || host == "localhost" || host == "127.0.0.1" || host == "" {
			return
		}
		if !services[host] {
			t.Errorf("%s names the host %q and the compose file has no such service, so it resolves to nothing", where, host)
		}
	}

	collector := readYAML[collectorConfig](t, "otel-collector.yaml")
	for name, exporter := range collector.Exporters {
		if exporter.Endpoint == "" {
			continue
		}
		check("the collector's "+name+" exporter", hostOf(exporter.Endpoint))
	}

	prometheus := readYAML[prometheusConfig](t, "prometheus.yaml")
	for _, scrape := range prometheus.ScrapeConfigs {
		for _, static := range scrape.StaticConfigs {
			for _, target := range static.Targets {
				check("the prometheus scrape job "+scrape.JobName, hostOf(target))
			}
		}
	}

	grafana := readYAML[grafanaConfig](t, "grafana/datasources.yaml")
	for _, datasource := range grafana.Datasources {
		check("the grafana data source "+datasource.Name, hostOf(datasource.URL))
	}

	if checked == 0 {
		t.Fatal("nothing in the stack names a host, which cannot be right: this test would pass with every config file emptied")
	}
}

// TestPrometheusScrapesWhereTheCollectorPublishes holds the two halves of the metrics path together.
// The collector publishes for scraping and Prometheus does the scraping, and the port lives in both
// files. Change one and metrics stop arriving with nothing to say so.
func TestPrometheusScrapesWhereTheCollectorPublishes(t *testing.T) {
	collector := readYAML[collectorConfig](t, "otel-collector.yaml")
	exporter, defined := collector.Exporters["prometheus"]
	if !defined {
		t.Fatal("the collector defines no prometheus exporter, so nothing republishes the crew's metrics for scraping")
	}
	published := portOf(exporter.Endpoint)
	if published == "" {
		t.Fatalf("the collector's prometheus exporter endpoint %q names no port", exporter.Endpoint)
	}

	prometheus := readYAML[prometheusConfig](t, "prometheus.yaml")
	for _, scrape := range prometheus.ScrapeConfigs {
		for _, static := range scrape.StaticConfigs {
			for _, target := range static.Targets {
				if hostOf(target) == "otel-collector" && portOf(target) == published {
					return
				}
			}
		}
	}
	t.Errorf("nothing scrapes otel-collector on port %s, which is where the collector publishes the crew's metrics", published)
}

// TestGrafanaIsProvisionedWithTheStacksOwnStores catches the state Grafana was in: four containers
// up, and Grafana holding no data source at all, so it opened onto nothing.
func TestGrafanaIsProvisionedWithTheStacksOwnStores(t *testing.T) {
	grafana := readYAML[grafanaConfig](t, "grafana/datasources.yaml")
	provisioned := map[string]bool{}
	for _, datasource := range grafana.Datasources {
		provisioned[hostOf(datasource.URL)] = true
	}
	for _, store := range []string{"tempo", "prometheus", "loki"} {
		if !provisioned[store] {
			t.Errorf("grafana has no data source pointed at %s, so what %s holds cannot be opened", store, store)
		}
	}
}

// TestTheStackMountsEveryConfigFileItNames is the sibling of the compose test's config file rule,
// for a service configured by a mount rather than by a command line flag. Grafana reads its
// provisioning directory whether or not anything put a file there.
func TestTheStackMountsEveryConfigFileItNames(t *testing.T) {
	for _, path := range []string{"otel-collector.yaml", "prometheus.yaml", "grafana/datasources.yaml"} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the compose file mounts %s and this repository does not carry it: %v", path, err)
		}
	}
}

// hostOf is the host part of an endpoint, which the stack spells as host:port or as a url.
func hostOf(endpoint string) string {
	trimmed := endpoint
	if index := strings.Index(trimmed, "://"); index >= 0 {
		trimmed = trimmed[index+3:]
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return host
	}
	return trimmed
}

// portOf is the port part, or empty when the endpoint names none.
func portOf(endpoint string) string {
	trimmed := endpoint
	if index := strings.Index(trimmed, "://"); index >= 0 {
		trimmed = trimmed[index+3:]
	}
	trimmed = strings.TrimSuffix(trimmed, "/")
	if _, port, err := net.SplitHostPort(trimmed); err == nil {
		return port
	}
	return ""
}
