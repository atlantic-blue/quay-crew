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

// TestNoServiceHidesBehindAProfile refuses the whole class rather than the four services that were
// behind one.
//
// Grafana, Loki, Tempo and Prometheus sat behind an "observability" profile and the broker behind an
// "export" one, so `make up` brought up a crew you could not see and an export nobody received. Each
// was a deliberate decision to keep a laptop light, and each cost more than it saved: the operator
// had to know a second command existed, and a signal nobody starts is a signal nobody has.
//
// A profile is how that comes back, so this refuses any of them.
func TestNoServiceHidesBehindAProfile(t *testing.T) {
	contents, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("reading the compose file: %v", err)
	}
	var compose struct {
		Services map[string]struct {
			Profiles []string `yaml:"profiles"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(contents, &compose); err != nil {
		t.Fatalf("parsing the compose file: %v", err)
	}
	if len(compose.Services) == 0 {
		t.Fatal("no services found in the compose file, so this test proves nothing")
	}
	for name, service := range compose.Services {
		if len(service.Profiles) > 0 {
			t.Errorf("service %s is behind the profile(s) %v, so a plain `make up` does not start it",
				name, service.Profiles)
		}
	}
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
	if len(config.Service.Pipelines) == 0 {
		t.Fatal("the collector defines no pipelines, so this test proves nothing")
	}

	for pipeline, spec := range config.Service.Pipelines {
		var keeps bool
		for _, exporter := range spec.Exporters {
			if exporter != "debug" {
				keeps = true
			}
		}
		if !keeps {
			t.Errorf("the %s pipeline exports to debug only, so what it receives is summarised into the collector's log and dropped",
				pipeline)
		}
	}
}

// TestEverySignalHasAStoreAndGrafanaCanOpenIt walks the whole path for each of the three signals,
// because each hop is in a different file and no one of them shows the break. A signal that reaches
// a store nothing is provisioned against is a signal nobody can look at.
func TestEverySignalHasAStoreAndGrafanaCanOpenIt(t *testing.T) {
	collector := readYAML[collectorConfig](t, "otel-collector.yaml")
	grafana := readYAML[grafanaConfig](t, "grafana/datasources.yaml")

	opens := map[string]bool{}
	for _, datasource := range grafana.Datasources {
		opens[hostOf(datasource.URL)] = true
	}

	// Metrics are the exception in shape rather than in coverage: the collector republishes them
	// and Prometheus pulls, so the pipeline names no Prometheus host to walk to.
	// TestPrometheusScrapesWhereTheCollectorPublishes covers that hop instead.
	stores := map[string]string{"traces": "tempo", "logs": "loki"}
	for signal, store := range stores {
		pipeline, defined := collector.Service.Pipelines[signal]
		if !defined {
			t.Errorf("the collector has no %s pipeline at all", signal)
			continue
		}
		var reaches bool
		for _, name := range pipeline.Exporters {
			if hostOf(collector.Exporters[name].Endpoint) == store {
				reaches = true
			}
		}
		if !reaches {
			t.Errorf("the %s pipeline reaches no exporter pointed at %s, so %s are exported and never stored",
				signal, store, signal)
		}
		if !opens[store] {
			t.Errorf("grafana has no data source pointed at %s, so the %s it stores cannot be opened", store, signal)
		}
	}
}

// TestALogLineInLokiLinksToItsTrace is the whole reason the correlation id exists. Without the
// derived field the id is a string an operator copies by hand, which is the same as not having it.
func TestALogLineInLokiLinksToItsTrace(t *testing.T) {
	contents, err := os.ReadFile("grafana/datasources.yaml")
	if err != nil {
		t.Fatalf("reading the grafana data sources: %v", err)
	}
	var grafana struct {
		Datasources []struct {
			Name     string `yaml:"name"`
			UID      string `yaml:"uid"`
			JSONData struct {
				DerivedFields []struct {
					MatcherRegex  string `yaml:"matcherRegex"`
					DatasourceUID string `yaml:"datasourceUid"`
				} `yaml:"derivedFields"`
			} `yaml:"jsonData"`
		} `yaml:"datasources"`
	}
	if err := yaml.Unmarshal(contents, &grafana); err != nil {
		t.Fatalf("parsing the grafana data sources: %v", err)
	}

	uids := map[string]bool{}
	for _, datasource := range grafana.Datasources {
		uids[datasource.UID] = true
	}

	for _, datasource := range grafana.Datasources {
		if datasource.UID != "loki" {
			continue
		}
		for _, field := range datasource.JSONData.DerivedFields {
			if field.MatcherRegex != "correlation_id" {
				continue
			}
			if !uids[field.DatasourceUID] {
				t.Errorf("the loki derived field links to the data source %q and nothing by that uid is provisioned",
					field.DatasourceUID)
			}
			return
		}
		t.Error("the loki data source has no derived field on correlation_id, so a log line does not link to its trace")
		return
	}
	t.Error("no loki data source is provisioned")
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

// authorityOf is the host and port of an endpoint, which the stack spells three ways: bare
// host:port, a url, and a url with a path on the end.
func authorityOf(endpoint string) string {
	trimmed := endpoint
	if index := strings.Index(trimmed, "://"); index >= 0 {
		trimmed = trimmed[index+3:]
	}
	if index := strings.Index(trimmed, "/"); index >= 0 {
		trimmed = trimmed[:index]
	}
	return trimmed
}

// hostOf is the host part, or the whole authority when it names no port.
func hostOf(endpoint string) string {
	authority := authorityOf(endpoint)
	if host, _, err := net.SplitHostPort(authority); err == nil {
		return host
	}
	return authority
}

// portOf is the port part, or empty when the endpoint names none.
func portOf(endpoint string) string {
	if _, port, err := net.SplitHostPort(authorityOf(endpoint)); err == nil {
		return port
	}
	return ""
}
