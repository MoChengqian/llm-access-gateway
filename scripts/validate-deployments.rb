#!/usr/bin/env ruby

require "open3"
require "psych"

REPO_ROOT = File.expand_path("..", __dir__)
COMPOSE_FILE = File.join(REPO_ROOT, "deployments", "docker", "docker-compose.yml")
OBSERVABILITY_COMPOSE_FILE = File.join(REPO_ROOT, "deployments", "observability", "docker-compose.yml")
K8S_DIR = File.join(REPO_ROOT, "deployments", "k8s")
OBSERVABILITY_DIR = File.join(REPO_ROOT, "deployments", "observability")
EXPECTED_NAMESPACE = "llm-access-gateway"

def main
  validate_compose(load_compose_config(COMPOSE_FILE))
  validate_observability_stack(load_compose_config(OBSERVABILITY_COMPOSE_FILE))
  validate_kubernetes_manifests
  puts "deployment validation passed"
end

def load_compose_config(path)
  stdout, stderr, status = Open3.capture3("docker", "compose", "-f", path, "config")
  unless status.success?
    abort("docker compose config failed: #{stderr.strip}")
  end

  Psych.safe_load(stdout, permitted_classes: [Symbol], aliases: false)
end

def validate_compose(compose)
  services = fetch_hash!(compose, "services", "compose config")
  expected_services = %w[mysql redis devinit gateway]
  expected_services.each do |service|
    abort("compose config missing service #{service}") unless services.key?(service)
  end

  gateway = services.fetch("gateway")
  mysql = services.fetch("mysql")
  redis = services.fetch("redis")
  devinit = services.fetch("devinit")

  validate_compose_dependency(gateway, "mysql", "service_healthy")
  validate_compose_dependency(gateway, "redis", "service_healthy")
  validate_compose_dependency(gateway, "devinit", "service_completed_successfully")
  validate_port_mapping(gateway, 8080)
  validate_healthcheck_contains(gateway, "http://127.0.0.1:8080/healthz")

  validate_port_mapping(mysql, 3306)
  validate_healthcheck_contains(mysql, "mysqladmin ping -h 127.0.0.1 -uuser -ppass --silent")

  validate_port_mapping(redis, 6379)
  validate_healthcheck_contains(redis, "redis-cli")

  command = Array(devinit["command"])
  abort("compose devinit command missing /app/devinit") unless command.include?("/app/devinit")
end

def validate_observability_stack(compose)
  services = fetch_hash!(compose, "services", "observability compose config")
  expected_services = ["otel-collector", "prometheus", "grafana"]
  expected_services.each do |service|
    abort("observability compose config missing service #{service}") unless services.key?(service)
  end

  collector = services.fetch("otel-collector")
  prometheus = services.fetch("prometheus")
  grafana = services.fetch("grafana")

  validate_port_mapping(collector, 4318)
  validate_port_mapping(collector, 13133)
  validate_port_mapping(collector, 8888)
  abort("otel collector command missing config path") unless Array(collector["command"]).include?("--config=/etc/otelcol-contrib/config.yaml")

  validate_compose_dependency(prometheus, "otel-collector", "service_started")
  validate_port_mapping(prometheus, 9090)
  abort("prometheus command missing config.file") unless Array(prometheus["command"]).include?("--config.file=/etc/prometheus/prometheus.yml")
  extra_hosts = Array(prometheus["extra_hosts"])
  abort("prometheus extra_hosts missing host.docker.internal") unless extra_hosts.any? { |entry| entry.to_s.include?("host.docker.internal") }

  validate_compose_dependency(grafana, "prometheus", "service_started")
  validate_port_mapping(grafana, 3000)
  environment = fetch_hash!(grafana, "environment", "grafana service")
  abort("grafana admin user mismatch") unless environment["GF_SECURITY_ADMIN_USER"] == "admin"
  abort("grafana admin password mismatch") unless environment["GF_SECURITY_ADMIN_PASSWORD"] == "admin"

  validate_observability_configs
end

def validate_compose_dependency(service, dependency, condition)
  depends_on = fetch_hash!(service, "depends_on", "compose service")
  details = fetch_hash!(depends_on, dependency, "compose depends_on")
  actual_condition = details["condition"]
  return if actual_condition == condition

  abort("compose dependency #{dependency} has condition #{actual_condition.inspect}, want #{condition.inspect}")
end

def validate_port_mapping(service, target_port)
  ports = Array(service["ports"])
  ok = ports.any? { |entry| entry.is_a?(Hash) && entry["target"].to_i == target_port }
  abort("compose service missing target port #{target_port}") unless ok
end

def validate_healthcheck_contains(service, expected_fragment)
  healthcheck = fetch_hash!(service, "healthcheck", "compose service")
  test = Array(healthcheck["test"]).join(" ")
  abort("compose healthcheck missing #{expected_fragment.inspect}") unless test.include?(expected_fragment)
end

def validate_kubernetes_manifests
  validate_namespace(load_manifest("namespace.yaml"))
  validate_configmap(load_manifest("configmap.yaml"))
  validate_secret(load_manifest("secret.example.yaml"))
  validate_job(load_manifest("job.yaml"))
  validate_deployment(load_manifest("deployment.yaml"))
  validate_service(load_manifest("service.yaml"))
end

def load_manifest(name)
  path = File.join(K8S_DIR, name)
  doc = Psych.safe_load(File.read(path), permitted_classes: [], aliases: false, filename: path)
  abort("manifest #{name} did not parse to a mapping") unless doc.is_a?(Hash)
  doc
end

def validate_namespace(doc)
  validate_kind(doc, "Namespace")
  validate_metadata_name(doc, EXPECTED_NAMESPACE)
end

def validate_configmap(doc)
  validate_kind(doc, "ConfigMap")
  validate_metadata_name(doc, "llm-access-gateway-config")
  validate_metadata_namespace(doc)
  data = fetch_hash!(doc, "data", "ConfigMap")
  %w[
    APP_SERVER_ADDRESS
    APP_LOG_LEVEL
    APP_OBSERVABILITY_SERVICE_NAME
    APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT
    APP_OBSERVABILITY_OTLP_TRACES_INSECURE
    APP_OBSERVABILITY_OTLP_EXPORT_TIMEOUT_SECONDS
    APP_REDIS_ADDRESS
    APP_GATEWAY_DEFAULT_MODEL
    APP_GATEWAY_PROVIDER_FAILURE_THRESHOLD
    APP_GATEWAY_PROVIDER_COOLDOWN_SECONDS
  ].each do |key|
    abort("ConfigMap missing #{key}") unless data.key?(key)
  end
end

def validate_secret(doc)
  validate_kind(doc, "Secret")
  validate_metadata_name(doc, "llm-access-gateway-secrets")
  validate_metadata_namespace(doc)
  string_data = fetch_hash!(doc, "stringData", "Secret")
  abort("Secret missing APP_MYSQL_DSN") unless string_data.key?("APP_MYSQL_DSN")
end

def validate_job(doc)
  validate_kind(doc, "Job")
  validate_metadata_name(doc, "llm-access-gateway-devinit")
  validate_metadata_namespace(doc)
  template_spec = fetch_hash!(fetch_hash!(fetch_hash!(doc, "spec", "Job"), "template", "Job template"), "spec", "Job template spec")
  abort("Job restartPolicy must be OnFailure") unless template_spec["restartPolicy"] == "OnFailure"
  container = first_container(template_spec, "Job")
  abort("Job container command missing /app/devinit") unless Array(container["command"]).include?("/app/devinit")
  validate_env_from_refs(container)
end

def validate_deployment(doc)
  validate_kind(doc, "Deployment")
  validate_metadata_name(doc, "llm-access-gateway")
  validate_metadata_namespace(doc)

  spec = fetch_hash!(doc, "spec", "Deployment")
  abort("Deployment replicas must be 1") unless spec["replicas"].to_i == 1
  selector = fetch_hash!(fetch_hash!(spec, "selector", "Deployment selector"), "matchLabels", "Deployment selector labels")
  abort("Deployment selector missing app label") unless selector["app"] == "llm-access-gateway"

  template_spec = fetch_hash!(fetch_hash!(spec, "template", "Deployment template"), "spec", "Deployment template spec")
  container = first_container(template_spec, "Deployment")
  abort("Deployment container image mismatch") unless container["image"] == "llm-access-gateway:latest"
  validate_container_port(container, 8080)
  validate_probe_path(container, "readinessProbe", "/readyz")
  validate_probe_path(container, "livenessProbe", "/healthz")
  validate_env_from_refs(container)
end

def validate_service(doc)
  validate_kind(doc, "Service")
  validate_metadata_name(doc, "llm-access-gateway")
  validate_metadata_namespace(doc)
  spec = fetch_hash!(doc, "spec", "Service")
  abort("Service type must be ClusterIP") unless spec["type"] == "ClusterIP"
  selector = fetch_hash!(spec, "selector", "Service selector")
  abort("Service selector missing app label") unless selector["app"] == "llm-access-gateway"
  ports = Array(spec["ports"])
  abort("Service missing port 8080") unless ports.any? { |entry| entry.is_a?(Hash) && entry["port"].to_i == 8080 }
end

def validate_observability_configs
  validate_otel_collector_config
  validate_prometheus_config
  validate_grafana_datasource_config
  validate_grafana_dashboard_config
end

def validate_otel_collector_config
  path = File.join(OBSERVABILITY_DIR, "otelcol", "config.yaml")
  doc = load_yaml(path)
  extensions = fetch_hash!(doc, "extensions", "otel collector config")
  health_check = fetch_hash!(extensions, "health_check", "otel collector extensions")
  abort("otel collector health endpoint mismatch") unless health_check["endpoint"] == "0.0.0.0:13133"

  receivers = fetch_hash!(doc, "receivers", "otel collector config")
  otlp = fetch_hash!(receivers, "otlp", "otel collector receivers")
  protocols = fetch_hash!(otlp, "protocols", "otel collector otlp receiver")
  http = fetch_hash!(protocols, "http", "otel collector otlp protocols")
  abort("otel collector http receiver endpoint mismatch") unless http["endpoint"] == "0.0.0.0:4318"

  exporters = fetch_hash!(doc, "exporters", "otel collector config")
  abort("otel collector missing debug exporter") unless exporters.key?("debug")

  service = fetch_hash!(doc, "service", "otel collector config")
  telemetry = fetch_hash!(service, "telemetry", "otel collector service")
  metrics = fetch_hash!(telemetry, "metrics", "otel collector telemetry")
  abort("otel collector telemetry metrics endpoint mismatch") unless metrics["address"] == "0.0.0.0:8888"

  pipelines = fetch_hash!(service, "pipelines", "otel collector service")
  traces = fetch_hash!(pipelines, "traces", "otel collector pipelines")
  abort("otel collector traces receiver mismatch") unless Array(traces["receivers"]).include?("otlp")
  abort("otel collector traces exporter mismatch") unless Array(traces["exporters"]).include?("debug")
end

def validate_prometheus_config
  path = File.join(OBSERVABILITY_DIR, "prometheus", "prometheus.yml")
  doc = load_yaml(path)
  scrape_configs = Array(doc["scrape_configs"])
  abort("prometheus config missing scrape_configs") if scrape_configs.empty?

  gateway_job = scrape_configs.find { |entry| entry.is_a?(Hash) && entry["job_name"] == "llm-access-gateway" }
  abort("prometheus config missing llm-access-gateway job") unless gateway_job
  abort("prometheus gateway metrics_path mismatch") unless gateway_job["metrics_path"] == "/metrics"
  gateway_targets = Array(Array(gateway_job["static_configs"]).first&.fetch("targets", []))
  abort("prometheus gateway target mismatch") unless gateway_targets.include?("host.docker.internal:8080")

  collector_job = scrape_configs.find { |entry| entry.is_a?(Hash) && entry["job_name"] == "otel-collector" }
  abort("prometheus config missing otel-collector job") unless collector_job
  collector_targets = Array(Array(collector_job["static_configs"]).first&.fetch("targets", []))
  abort("prometheus collector target mismatch") unless collector_targets.include?("otel-collector:8888")
end

def validate_grafana_datasource_config
  path = File.join(OBSERVABILITY_DIR, "grafana", "provisioning", "datasources", "datasources.yaml")
  doc = load_yaml(path)
  datasources = Array(doc["datasources"])
  abort("grafana datasources config missing datasources") if datasources.empty?
  prometheus = datasources.find { |entry| entry.is_a?(Hash) && entry["name"] == "Prometheus" }
  abort("grafana datasources missing Prometheus") unless prometheus
  abort("grafana datasource type mismatch") unless prometheus["type"] == "prometheus"
  abort("grafana datasource url mismatch") unless prometheus["url"] == "http://prometheus:9090"
end

def validate_grafana_dashboard_config
  path = File.join(OBSERVABILITY_DIR, "grafana", "provisioning", "dashboards", "dashboards.yaml")
  doc = load_yaml(path)
  providers = Array(doc["providers"])
  abort("grafana dashboards config missing providers") if providers.empty?
  provider = providers.find { |entry| entry.is_a?(Hash) && entry["name"] == "llm-access-gateway" }
  abort("grafana dashboards provider missing llm-access-gateway") unless provider
  options = fetch_hash!(provider, "options", "grafana dashboard provider")
  expected_path = "/var/lib/grafana/dashboards/llm-access-gateway"
  abort("grafana dashboard provisioning path mismatch") unless options["path"] == expected_path

  dashboard_json = File.join(REPO_ROOT, "deployments", "grafana", "dashboards", "llm-access-gateway.json")
  abort("grafana dashboard JSON missing at #{dashboard_json}") unless File.file?(dashboard_json)
end

def validate_kind(doc, expected_kind)
  actual = doc["kind"]
  return if actual == expected_kind

  abort("manifest kind #{actual.inspect}, want #{expected_kind.inspect}")
end

def validate_metadata_name(doc, expected_name)
  metadata = fetch_hash!(doc, "metadata", doc["kind"] || "manifest")
  actual = metadata["name"]
  return if actual == expected_name

  abort("#{doc['kind']} metadata.name #{actual.inspect}, want #{expected_name.inspect}")
end

def validate_metadata_namespace(doc)
  metadata = fetch_hash!(doc, "metadata", doc["kind"] || "manifest")
  actual = metadata["namespace"]
  return if actual == EXPECTED_NAMESPACE

  abort("#{doc['kind']} metadata.namespace #{actual.inspect}, want #{EXPECTED_NAMESPACE.inspect}")
end

def validate_container_port(container, expected_port)
  ports = Array(container["ports"])
  ok = ports.any? { |entry| entry.is_a?(Hash) && entry["containerPort"].to_i == expected_port }
  abort("container missing port #{expected_port}") unless ok
end

def validate_probe_path(container, probe_key, expected_path)
  probe = fetch_hash!(container, probe_key, "container")
  http_get = fetch_hash!(probe, "httpGet", probe_key)
  actual = http_get["path"]
  return if actual == expected_path

  abort("#{probe_key} path #{actual.inspect}, want #{expected_path.inspect}")
end

def validate_env_from_refs(container)
  refs = Array(container["envFrom"])
  configmap_ok = refs.any? { |entry| entry.is_a?(Hash) && entry.dig("configMapRef", "name") == "llm-access-gateway-config" }
  secret_ok = refs.any? { |entry| entry.is_a?(Hash) && entry.dig("secretRef", "name") == "llm-access-gateway-secrets" }
  abort("container envFrom missing llm-access-gateway-config") unless configmap_ok
  abort("container envFrom missing llm-access-gateway-secrets") unless secret_ok
end

def first_container(template_spec, label)
  containers = Array(template_spec["containers"])
  abort("#{label} missing containers") if containers.empty?

  containers.first
end

def fetch_hash!(value, key, label)
  hash = value[key]
  abort("#{label} missing #{key}") unless hash.is_a?(Hash)

  hash
end

def load_yaml(path)
  doc = Psych.safe_load(File.read(path), permitted_classes: [], aliases: false, filename: path)
  abort("YAML file #{path} did not parse to a mapping") unless doc.is_a?(Hash)
  doc
end

main
