#!/usr/bin/env ruby

require "json"
require "net/http"
require "optparse"
require "uri"

DEFAULT_PROJECT = "MoChengqian_llm-access-gateway"
DEFAULT_BRANCH = "main"
SONAR_BASE_URL = "https://sonarcloud.io"

def fetch_json(path, params)
  uri = URI("#{SONAR_BASE_URL}#{path}")
  uri.query = URI.encode_www_form(params)

  response = Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == "https") do |http|
    request = Net::HTTP::Get.new(uri)
    request["Accept"] = "application/json"
    http.request(request)
  end

  unless response.is_a?(Net::HTTPSuccess)
    warn "SonarCloud API request failed: #{uri} -> #{response.code} #{response.message}"
    exit 3
  end

  JSON.parse(response.body)
end

options = {
  project: DEFAULT_PROJECT,
  branch: DEFAULT_BRANCH,
  report_only: false
}

OptionParser.new do |parser|
  parser.banner = "Usage: #{File.basename($PROGRAM_NAME)} [options]"

  parser.on("--project KEY", "SonarCloud project key (default: #{DEFAULT_PROJECT})") do |value|
    options[:project] = value
  end

  parser.on("--branch NAME", "Branch name to inspect (default: #{DEFAULT_BRANCH})") do |value|
    options[:branch] = value
  end

  parser.on("--pull-request NUMBER", "Pull request number to inspect instead of a branch") do |value|
    options[:pull_request] = value
  end

  parser.on("--report-only", "Print the status without failing on non-OK states") do
    options[:report_only] = true
  end
end.parse!

status_params =
  if options[:pull_request]
    { projectKey: options[:project], pullRequest: options[:pull_request] }
  else
    { projectKey: options[:project], branch: options[:branch] }
  end

scope_label =
  if options[:pull_request]
    "pull request ##{options[:pull_request]}"
  else
    "branch #{options[:branch]}"
  end

component = fetch_json("/api/navigation/component", component: options[:project])
project_status = fetch_json("/api/qualitygates/project_status", status_params)["projectStatus"]

quality_gate = component.fetch("qualityGate", {})
quality_gate_name = quality_gate.fetch("name", "unknown")
analysis_date = component["analysisDate"] || "unknown"
ci_name = component["ciName"] || "unknown"
status = project_status.fetch("status", "UNKNOWN")

puts "SonarCloud project: #{options[:project]}"
puts "Scope: #{scope_label}"
puts "CI source: #{ci_name}"
puts "Assigned quality gate: #{quality_gate_name}"
puts "Last analysis: #{analysis_date}"
puts "Quality gate status: #{status}"

project_url = "#{SONAR_BASE_URL}/dashboard?id=#{options[:project]}"
project_url += "&branch=#{URI.encode_www_form_component(options[:branch])}" unless options[:pull_request]
project_url += "&pullRequest=#{URI.encode_www_form_component(options[:pull_request])}" if options[:pull_request]
puts "Dashboard: #{project_url}"

exit 0 if status == "OK"

if status == "NONE"
  analyses = fetch_json("/api/project_analyses/search", project: options[:project], branch: options[:branch], ps: 10)
  analysis_count = analyses.fetch("paging", {}).fetch("total", 0)

  warn "SonarCloud did not compute a quality gate for #{scope_label}."
  if options[:pull_request]
    warn "This usually means the PR analysis is not fully available yet."
  elsif analysis_count > 1
    warn "The #{options[:branch]} branch already has #{analysis_count} analyses, so this is not a first-analysis warmup case."
    warn "Most likely the SonarCloud project still needs a New Code Definition for the main branch."
  else
    warn "This can happen on the very first branch analysis before a baseline exists."
  end
  warn "Open the SonarCloud project admin UI and set a New Code Definition for #{options[:branch]}, then rerun analysis."
  warn "Suggested path: Administration -> New Code."
end

project_status.fetch("conditions", []).each do |condition|
  warn "#{condition.fetch("metricKey", "unknown")}: #{condition.fetch("status", "UNKNOWN")} (actual=#{condition["actualValue"]}, threshold=#{condition["errorThreshold"]})"
end

exit 0 if options[:report_only]
exit(status == "NONE" ? 2 : 1)
