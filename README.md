# pagerduty-krm-extensions

Kubernetes kinds for the PagerDuty configuration that Terraform's
`PagerDuty/pagerduty` provider manages and PagerDuty's operators do not. PagerDuty publishes no Kubernetes operator for its configuration; services, schedules and escalation policies are managed through its API or Terraform.

By the numbers: `PagerDuty/pagerduty` is the #30 Terraform provider by downloads
(164,117,336); version 3.37.0 has 51 resources. Operators
cover 0. This repository adds **46 kinds** standing in for
**46 of them**. Each gap kind is listed below with its Terraform equivalent;
the 5 it does not cover, and why, are under "Not covered".

**Limits.** Request and response bodies are wrapped in the resource's name ({"service": {...}}); the spec holds the inner object.

**Readiness: 27 of 46 kinds also proven against the real API** (a PagerDuty
developer account). Every kind passes create, update, drift repair and delete
against a fake built from the same descriptors (`TestEveryKindOnFake`). The
kinds in `hack/live/live.yaml` run the same steps against the real API
(`TestEveryKindLive`, built only with `-tags live`); `hack/live/records.json`
holds each one's last result, and `zz_generated.report.md` marks those that
passed `live`. Treat the others as untested against the real API.
`ServiceIntegration`, `Enablement`, `BusinessServiceDependency` and
`TechnicalServiceDependency` are in `live.yaml` but not yet run live (`Enablement`'s only feature, AIOps, is a paid add-on; the API
documents that an account without it still reads and sets the switch, with
a warning). Why each of the 12 others is not run: Automation Actions (5 kinds) is a paid add-on the developer
account lacks; the user kinds (4) and `TeamMembership` need a test user, and
the account allows only its own domain's (deliverable) addresses; an incident
type cannot be deleted, so each run would leave one behind; a Slack
connection needs a Slack workspace connected to the account. `hack/live/live.yaml`
lists every kind, with why each one not run is skipped.

**PagerDuty specifics.** `type` (`tag`, `service`, ...) is defaulted in each
CRD and sent, as PagerDuty requires it. Custom fields' `name`, `data_type`
and `field_type`, and a few others per kind (`createOnly` in `types.yaml`),
are set at create only: change one by re-creating the object. A schedule's
layers are updated in place (`hooks.go` gives each layer the ID PagerDuty
gave it); give layers a `name` when a schedule has more than one.

A ServiceNow extension (`pagerduty_extension_servicenow`) is an `Extension`
whose `extension_schema` is ServiceNow's, with its settings in `config`
(`snow_user`, `snow_password`, `sync_options`, `target`, `task_type`,
`referer`). PagerDuty does not read the password back, so a change to
`config.snow_password` alone is sent once, not compared.

`ServiceIntegration`'s calls are PagerDuty's REST spec's, plus the `DELETE
/services/{id}/integrations/{integration_id}` the spec leaves out and the
Terraform provider uses (`hack/gen/supplement/service-integrations.yaml`).

`IncidentType` (`pagerduty_incident_type`): PagerDuty's API creates, reads
and updates incident types but cannot delete one. Deleting the object lets
go of the incident type and leaves it in PagerDuty, as the Terraform
provider does; set `enabled: false` first to retire it. `name` and
`parent_type` are set at create only.

`TeamMembership` (`pagerduty_team_membership`) is written with `PUT` and
removed with `DELETE /teams/{id}/users/{user_id}`; PagerDuty has no call
that reads one membership, so it is read from the team's member list
(`GET /teams/{id}/members`, the entry whose `user.id` is the user), as the
Terraform provider reads it. The list is read one page of 100: a membership
in a team of more than 100 members may not be found. Unlike the provider,
which defaults `role` to `manager`, an unset `role` is left to PagerDuty.

`Enablement` (`pagerduty_enablement`) is a service's feature enablement
(`PUT /services/{id}/enablements/{feature_name}`, feature `aiops`), read
from the service's list of enablements. It is a settings kind: deleting
the object leaves the setting as it is (the provider sets `enabled: false`
on delete). The provider's other entity type, an event orchestration's
enablement (`/event_orchestrations/{id}/enablements/{feature_name}`), has
no kind yet.

### Slack connections

`SlackConnection` (`pagerduty_slack_connection`) is PagerDuty's Slack
integration API (its own spec, `integration-slack-service`), whose paths are
relative to `https://app.pagerduty.com/integration-slack` (the Terraform
provider's base for it; EU accounts: `https://app.eu.pagerduty.com/integration-slack`),
not the REST API's base URL. It needs a **user-level** API token (a personal
REST API key), not an account key, sent the same way. Give such objects a
second Connection and name it in `spec.connectionRef`:

```yaml
apiVersion: v1
kind: Secret
metadata: {name: pagerduty-user-token, namespace: team-a}
stringData: {token: <user-level token>}
---
apiVersion: core.pagerduty.hfl.dev/v1alpha1
kind: Connection
metadata: {name: slack, namespace: team-a}
spec:
  baseURL: https://app.pagerduty.com/integration-slack
  secretRef: {name: pagerduty-user-token}
---
apiVersion: slack.pagerduty.hfl.dev/v1alpha1
kind: SlackConnection
metadata: {name: alerts, namespace: team-a}
spec:
  connectionRef: {name: slack}
  slackTeamID: <Slack workspace ID>
  source_id: <service ID>
  source_type: service_reference
  channel_id: <Slack channel ID>
  notification_type: responder
  config: {events: [incident.triggered, incident.resolved]}
```

## How it works

The kinds are generated, not hand-written. `hack/gen/vendor.yaml` pins
PagerDuty's OpenAPI spec; [krm-rest-runtime](https://github.com/highfivelabs/krm-rest-runtime)'s
generator finds every resource the API exposes in a create/read/update/delete
shape, keeps those that stand in for a Terraform resource in the gap list
(`hack/gen/terraform-gaps.json`), and writes:

- `types.yaml`: per kind, its API calls, path parameters and Terraform equivalents
- `config/crds/`: one CRD per kind, its spec the API's request body
- `zz_generated.report.md`: the full list

One generic manager (`cmd/manager`) runs every kind on krm-rest-runtime's
reconciler: it creates the resource, reads it back every 10 minutes and
puts back anything changed outside Kubernetes, and deletes it with the
object. Spec fields keep PagerDuty's own names, so the API reference
applies field for field. Path parameters are spec fields too.

Hand corrections to the automatic matching (a Terraform type matched to
the wrong API path, or not matched) are in `vendor.yaml`'s `overrides`.

## Install

```sh
kubectl kustomize config/default | KO_DOCKER_REPO=<registry> ko resolve -f - | kubectl apply --server-side -f -
```

Then, in each namespace that manages PagerDuty, a Secret with
`token` and a Connection named `default`
(`config/samples/connection.yaml`). Objects use it unless
`spec.connectionRef.name` names another. Secret values (tokens, passwords,
secret values) go in a Kubernetes Secret, not the spec:
`spec.secretRefs: {<field>: {name, key}}` fills that body field.

To run only some API groups, install those groups' CRDs from `config/crds`
and pass `--groups` to the manager.

## Kinds

| Kind | Terraform equivalent | Shape | Read |
| --- | --- | --- | --- |
| `addon.pagerduty.hfl.dev/v1alpha1` `Addon` | `pagerduty_addon` | crud | `GET /addons/{id}` |
| `alert.pagerduty.hfl.dev/v1alpha1` `AlertGroupingSetting` | `pagerduty_alert_grouping_setting` | crud | `GET /alert_grouping_settings/{id}` |
| `automation.pagerduty.hfl.dev/v1alpha1` `AutomationActionsAction` | `pagerduty_automation_actions_action` | crud | `GET /automation_actions/actions/{id}` |
| `automation.pagerduty.hfl.dev/v1alpha1` `AutomationActionsActionServiceAssociation` | `pagerduty_automation_actions_action_service_association` | crud | `GET /automation_actions/actions/{id}/services/{service_id}` |
| `automation.pagerduty.hfl.dev/v1alpha1` `AutomationActionsActionTeamAssociation` | `pagerduty_automation_actions_action_team_association` | crud | `GET /automation_actions/actions/{id}/teams/{team_id}` |
| `automation.pagerduty.hfl.dev/v1alpha1` `AutomationActionsRunner` | `pagerduty_automation_actions_runner` | crud | `GET /automation_actions/runners/{id}` |
| `automation.pagerduty.hfl.dev/v1alpha1` `AutomationActionsRunnerTeamAssociation` | `pagerduty_automation_actions_runner_team_association` | crud | `GET /automation_actions/runners/{id}/teams/{team_id}` |
| `business.pagerduty.hfl.dev/v1alpha1` `BusinessService` | `pagerduty_business_service` | crud | `GET /business_services/{id}` |
| `business.pagerduty.hfl.dev/v1alpha1` `BusinessServiceSubscriber` | `pagerduty_business_service_subscriber` | assoc | `GET /business_services/{id}/subscribers` |
| `enablement.pagerduty.hfl.dev/v1alpha1` `Enablement` | `pagerduty_enablement` (services) | settings | `GET /services/{id}/enablements` (the entry whose `feature` is `{feature_name}`) |
| `escalation.pagerduty.hfl.dev/v1alpha1` `EscalationPolicy` | `pagerduty_escalation_policy` | crud | `GET /escalation_policies/{id}` |
| `event.pagerduty.hfl.dev/v1alpha1` `EventOrchestration` | `pagerduty_event_orchestration` | crud | `GET /event_orchestrations/{id}` |
| `event.pagerduty.hfl.dev/v1alpha1` `EventOrchestrationGlobal` | `pagerduty_event_orchestration_global` | settings | `GET /event_orchestrations/{id}/global` |
| `event.pagerduty.hfl.dev/v1alpha1` `EventOrchestrationGlobalCacheVariable` | `pagerduty_event_orchestration_global_cache_variable` | crud | `GET /event_orchestrations/{id}/cache_variables/{cache_variable_id}` |
| `event.pagerduty.hfl.dev/v1alpha1` `EventOrchestrationIntegration` | `pagerduty_event_orchestration_integration` | crud | `GET /event_orchestrations/{id}/integrations/{integration_id}` |
| `event.pagerduty.hfl.dev/v1alpha1` `EventOrchestrationRouter` | `pagerduty_event_orchestration_router` | settings | `GET /event_orchestrations/{id}/router` |
| `event.pagerduty.hfl.dev/v1alpha1` `EventOrchestrationService` | `pagerduty_event_orchestration_service` | settings | `GET /event_orchestrations/services/{service_id}` |
| `event.pagerduty.hfl.dev/v1alpha1` `EventOrchestrationServiceCacheVariable` | `pagerduty_event_orchestration_service_cache_variable` | crud | `GET /event_orchestrations/services/{service_id}/cache_variables/{cache_variable_id}` |
| `event.pagerduty.hfl.dev/v1alpha1` `EventOrchestrationUnrouted` | `pagerduty_event_orchestration_unrouted` | settings | `GET /event_orchestrations/{id}/unrouted` |
| `extension.pagerduty.hfl.dev/v1alpha1` `Extension` | `pagerduty_extension`, `pagerduty_extension_servicenow` | crud | `GET /extensions/{id}` |
| `incident.pagerduty.hfl.dev/v1alpha1` `IncidentCustomField` | `pagerduty_incident_custom_field` | crud | `GET /incidents/custom_fields/{field_id}` |
| `incident.pagerduty.hfl.dev/v1alpha1` `IncidentCustomFieldOption` | `pagerduty_incident_custom_field_option` | crud | `GET /incidents/types/{type_id_or_name}/custom_fields/{field_id}/field_options/{field_option_id}` |
| `incident.pagerduty.hfl.dev/v1alpha1` `IncidentType` | `pagerduty_incident_type` | crud (no delete: let go) | `GET /incidents/types/{type_id_or_name}` |
| `incident.pagerduty.hfl.dev/v1alpha1` `IncidentTypeCustomField` | `pagerduty_incident_type_custom_field` | crud | `GET /incidents/types/{type_id_or_name}/custom_fields/{field_id}` |
| `incident.pagerduty.hfl.dev/v1alpha1` `IncidentWorkflow` | `pagerduty_incident_workflow` | crud | `GET /incident_workflows/{id}` |
| `incident.pagerduty.hfl.dev/v1alpha1` `IncidentWorkflowTrigger` | `pagerduty_incident_workflow_trigger` | crud | `GET /incident_workflows/triggers/{id}` |
| `jira.pagerduty.hfl.dev/v1alpha1` `JiraCloudAccountMappingRule` | `pagerduty_jira_cloud_account_mapping_rule` | crud | `GET /integration-jira-cloud/accounts_mappings/{id}/rules/{rule_id}` |
| `maintenance.pagerduty.hfl.dev/v1alpha1` `MaintenanceWindow` | `pagerduty_maintenance_window` | crud | `GET /maintenance_windows/{id}` |
| `schedule.pagerduty.hfl.dev/v1alpha1` `Schedule` | `pagerduty_schedule` | crud | `GET /schedules/{id}` |
| `schedulev2.pagerduty.hfl.dev/v1alpha1` `Schedulev2` | `pagerduty_schedulev2` | crud | `GET /v3/schedules/{id}` |
| `service.pagerduty.hfl.dev/v1alpha1` `BusinessServiceDependency` | `pagerduty_service_dependency` | crud | `GET /service_dependencies/business_services/{dependentId}` (finds `id`) |
| `service.pagerduty.hfl.dev/v1alpha1` `Service` | `pagerduty_service` | crud | `GET /services/{id}` |
| `service.pagerduty.hfl.dev/v1alpha1` `ServiceCustomField` | `pagerduty_service_custom_field` | crud | `GET /services/custom_fields/{field_id}` |
| `service.pagerduty.hfl.dev/v1alpha1` `ServiceCustomFieldValue` | `pagerduty_service_custom_field_value` | settings | `GET /services/{id}/custom_fields/values` |
| `service.pagerduty.hfl.dev/v1alpha1` `ServiceIntegration` | `pagerduty_service_integration` | crud | `GET /services/{id}/integrations/{integration_id}` |
| `service.pagerduty.hfl.dev/v1alpha1` `TechnicalServiceDependency` | `pagerduty_service_dependency` | crud | `GET /service_dependencies/technical_services/{dependentId}` (finds `id`) |
| `slack.pagerduty.hfl.dev/v1alpha1` `SlackConnection` | `pagerduty_slack_connection` | crud | `GET /workspaces/{slack_team_id}/connections/{connection_id}` |
| `tag.pagerduty.hfl.dev/v1alpha1` `Tag` | `pagerduty_tag` | crud | `GET /tags/{id}` |
| `tag.pagerduty.hfl.dev/v1alpha1` `TagAssignment` | `pagerduty_tag_assignment` | assoc | `GET /{entity_type}/{id}/tags` |
| `team.pagerduty.hfl.dev/v1alpha1` `Team` | `pagerduty_team` | crud | `GET /teams/{id}` |
| `team.pagerduty.hfl.dev/v1alpha1` `TeamMembership` | `pagerduty_team_membership` | upsert | `GET /teams/{id}/members` (the entry whose `user.id` is `{user_id}`) |
| `user.pagerduty.hfl.dev/v1alpha1` `User` | `pagerduty_user` | crud | `GET /users/{id}` |
| `user.pagerduty.hfl.dev/v1alpha1` `UserContactMethod` | `pagerduty_user_contact_method` | crud | `GET /users/{id}/contact_methods/{contact_method_id}` |
| `user.pagerduty.hfl.dev/v1alpha1` `UserHandoffNotificationRule` | `pagerduty_user_handoff_notification_rule` | crud | `GET /users/{id}/oncall_handoff_notification_rules/{oncall_handoff_notification_rule_id}` |
| `user.pagerduty.hfl.dev/v1alpha1` `UserNotificationRule` | `pagerduty_user_notification_rule` | crud | `GET /users/{id}/notification_rules/{notification_rule_id}` |
| `webhook.pagerduty.hfl.dev/v1alpha1` `WebhookSubscription` | `pagerduty_webhook_subscription` | crud | `GET /webhook_subscriptions/{id}` |

`BusinessServiceSubscriber` and `TagAssignment` are links: made by a POST of
a one-element list (`{"subscribers": [...]}`; `change_tags`'
`{"add": [...]}`), found in the list a read answers, and undone by the same
element to `.../unsubscribe` or in `change_tags`' `{"remove": [...]}`. A
`TagAssignment` names its entity by `spec.entityType` (`users`, `teams` or
`escalation_policies`) and `spec.entityID`, and its tag as a reference:
`type: tag_reference` and the tag's `id` (the remove takes only a
reference); only the id is compared with what the read answers.

## Not covered

<!-- BEGIN generated: not covered (krmgen.py, from vendor.yaml hardBlocks and draftNotes) -->
5 of the 51 Terraform gap types have no kind: 5 are not built (`hardBlocks` in `hack/gen/vendor.yaml`).

| Terraform | Status | Why |
| --- | --- | --- |
| `pagerduty_event_rule` | not built | A retired API (/event_rules): terraform-provider-pagerduty v3.37.0 deprecates the type in favor of rulesets (website/docs/r/event_rule), which are end-of-life too, and PagerDuty's REST spec no longer has the path. |
| `pagerduty_response_play` | not built | Removed from PagerDuty's API on 2025-09-09 (the provider's docs: "Response Play will end-of-life soon"); incident workflows (IncidentWorkflow) replace response plays. |
| `pagerduty_ruleset` | not built | End-of-life: the provider's docs say rulesets and event rules "will end-of-life soon" (use Event Orchestration), and PagerDuty's API refuses new ones (HTTP 405 "Creating new rulesets and rules is not allowed"). |
| `pagerduty_ruleset_rule` | not built | End-of-life, as pagerduty_ruleset: the API refuses new ruleset rules (HTTP 405). |
| `pagerduty_service_event_rule` | not built | End-of-life, as pagerduty_ruleset: the API refuses new service event rules (HTTP 405); Event Orchestration's service rules (EventOrchestrationService) replace them. |
<!-- END generated: not covered -->

`pagerduty_service_dependency` is two kinds, by the dependent service's
type, since PagerDuty lists a service's dependencies at
`/service_dependencies/business_services/{id}` or
`.../technical_services/{id}`: `BusinessServiceDependency` and
`TechnicalServiceDependency`. Each is made by
`POST /service_dependencies/associate` and undone by the same body to
`.../disassociate`; nothing can be changed. The services are read back with
other type names (`business_service_reference`), so they are not compared.

## For agents (MCP)

`cmd/mcp` serves this repository's kinds over the Model Context Protocol
(stdio), from `types.yaml`, `config/crds` and the live tests' records.
`.mcp.json` registers it, so an agent session opened in this repository
has it without setup. Elsewhere, run it from the repository root:

```sh
go run ./cmd/mcp
```

Its tools: `list_kinds`, `find_kinds_for_terraform`, `describe_kind` (every
field, the REST calls, the live record), `example` (the live-tested spec
where there is one), `validate_manifest` (the CRD schema, as the API server
checks it) and `plan_on_fake` (the controller run against the fake API,
with every REST call it makes). Nothing it does reaches the real API.
krm-rest-runtime's README ("For agents") has the details.

## Regenerate

```sh
hack/gen/generate.sh   # needs python3 + pyyaml, and krm-rest-runtime beside this repo
go test ./...
```
