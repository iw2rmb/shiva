# Vacuum Results

## Scope
This note captures a first `vacuum` run against OpenAPI specs stored in Shiva's local database.

Related documents:
- [OpenAPI validation frameworks](./openapi-validation-frameworks.md)
- [OpenAPI validation architecture](../design/openapi-validation-architecture.md)
- [Current runtime and query contract](../docs/endpoints.md)

Run date:
- 2026-03-12

Environment:
- database: `postgres://localhost/shiva`
- tool: `vacuum v0.25.0`

## Method
Sample selection:
- take the latest `processed` `spec_artifacts` row per namespace
- order by namespace
- select the first 10 namespaces

Practical flow:
1. query `(namespace, repo, root_path, api_spec_revision_id, sha)` from Postgres
2. export `spec_yaml` for each selected `api_spec_revision_id`
3. run `vacuum report <spec> --stdout --no-style`
4. aggregate scores, severities, and top rule IDs from the JSON reports

## Sample
| Namespace | Repo | Root Path | Revision | SHA | Status | Format |
|---|---|---|---:|---|---|---|
| `ai-platform` | `ml-core-public-api` | `mlcore/openapi/v1beta/users.swagger.json` | 697 | `b0db86b9` | `ok` | `oas2 2.0` |
| `allure` | `allure-deployment` | `service-catalog/allure-api.yaml` | 39 | `3112ba88` | `ok` | `oas3 3.0.1` |
| `anomaly-analyzer` | `king-crimson` | `gateway/src/main/resources/swagger.yaml` | 53 | `81aa4349` | `ok` | `oas3 3.0.0` |
| `api-common` | `wespa` | `api-schemas/openapi-v1-websocket_fallback.yaml` | 375 | `531698ad` | `ok` | `oas3 3.0.1` |
| `appsec` | `burpsy` | `app/api/swagger.yml` | 129 | `54382deb` | `ok` | `oas2 2.0` |
| `atmapi` | `atmapi-cleaning-management` | `src/main/resources/schemas/api/atmapi-cleaning-management.yaml` | 528 | `a5a585bd` | `ok` | `oas3 3.0.1` |
| `bds` | `wizard` | `botqm/spec/openapi.yaml` | 595 | `b5b42c3e` | `ok` | `oas3_1 3.1.0` |
| `bigops-sre` | `cdtest` | `swagger_docs/swagger.yml` | 743 | `bade2c93` | `ok` | `oas3 3.0.3` |
| `bods` | `bods-task-service` | `app/configs/app.yaml` | 148 | `de24c8e0` | `failed` | `-` |
| `caen` | `tma-tests` | `src/main/resources/openapi/tma-caen-api/tma-caen-api.yaml` | 370 | `7d0cf973` | `ok` | `oas3 3.0.3` |

## Aggregate Results
Across the 10 sampled namespaces:
- selected specs: `10`
- successful `vacuum` reports: `9`
- parser failures: `1`
- average score on successful reports: `44.11`
- min score: `10`
- max score: `98`
- specs with errors: `7/9`
- specs with warnings: `9/9`
- successful OAS2 specs: `2`
- successful OAS3 or OAS3.1 specs: `7`

Severity totals across successful reports:
- errors: `29`
- warnings: `4995`
- info: `1675`

## Top Rules
Most common rule IDs across the 9 successful runs:

| Rule | Count |
|---|---:|
| `oas3-missing-example` | 1704 |
| `description-duplication` | 1663 |
| `component-description` | 917 |
| `oas3-parameter-description` | 768 |
| `operation-tag-defined` | 593 |
| `operation-description` | 511 |
| `no-unnecessary-combinator` | 213 |
| `oas3-valid-schema-example` | 89 |
| `paths-kebab-case` | 79 |
| `oas3-unused-component` | 58 |
| `camel-case-properties` | 47 |
| `oas-schema-check` | 13 |
| `oas-missing-type` | 10 |
| `operation-operationId` | 8 |
| `no-$ref-siblings` | 5 |

Most affected categories:

| Category | Issues |
|---|---:|
| `Descriptions` | 3863 |
| `Examples` | 1793 |
| `Tags` | 597 |
| `Schemas` | 348 |
| `Operations` | 90 |
| `Contract Information` | 6 |
| `Validation` | 2 |

## Per-Spec Summary
| Namespace | Score | Errors | Warnings | Info | Dominant Rules |
|---|---:|---:|---:|---:|---|
| `ai-platform` | 67 | 2 | 5 | 7 | `description-duplication (6)`, `oas2-parameter-description (4)`, `no-$ref-siblings (2)` |
| `allure` | 25 | 0 | 3504 | 1375 | `description-duplication (1375)`, `oas3-missing-example (1340)`, `oas3-parameter-description (699)` |
| `anomaly-analyzer` | 14 | 4 | 79 | 21 | `description-duplication (21)`, `component-description (19)`, `oas3-missing-example (17)` |
| `api-common` | 84 | 1 | 3 | 0 | `oas3-missing-example (1)`, `operation-operationId (1)`, `operation-tag-defined (1)` |
| `appsec` | 98 | 0 | 1 | 12 | `description-duplication (11)`, `oas2-api-host (1)`, `oas2-api-schemes (1)` |
| `atmapi` | 51 | 1 | 108 | 16 | `oas3-missing-example (33)`, `operation-description (22)`, `oas3-parameter-description (19)` |
| `bds` | 10 | 10 | 225 | 31 | `oas3-valid-schema-example (83)`, `oas3-missing-example (37)`, `camel-case-properties (33)` |
| `bigops-sre` | 38 | 4 | 4 | 0 | `operation-operationId (4)`, `operation-tags (4)` |
| `bods` | - | - | - | - | parser failure |
| `caen` | 10 | 7 | 1066 | 213 | `component-description (347)`, `oas3-missing-example (276)`, `description-duplication (213)` |

## Parser Failure
`vacuum` failed completely on one stored artifact:
- namespace: `bods`
- repo: `bods-task-service`
- root path: `app/configs/app.yaml`
- revision: `148`
- error: `failed to parse specification`

The exported artifact content begins like this:

```yaml
app:
  batch_length: ${TASK_SERVICE_BATCH_LENGTH}
  engine_api_url: ${BODS_ENGINE_API_URL}
  heartbeat_service_url: ${BODS_HEARTBEAT_SERVICE_URL}
```

Inference:
- this artifact is not an OpenAPI document at all
- either candidate detection or persisted artifact integrity is allowing non-OpenAPI config files into `spec_artifacts`

This is a higher-signal finding than most lint output because it points to a data-quality or ingest-contract bug.

## What This Says About Vacuum For Shiva
Useful:
- it runs on Shiva's exported canonical specs without extra translation
- it handles OAS2, OAS3, and at least one OAS3.1 sample in this corpus
- it produces stable rule IDs and structured JSON reports
- it surfaced one hard parser failure that Shiva should investigate

Noisy:
- the default ruleset is extremely chatty on real specs in this database
- `Descriptions` and `Examples` dominate the issue volume
- large specs produce thousands of warnings and info findings, which would drown operator signal if emitted raw

Implications for Shiva:
- `vacuum` is viable as the static lint engine
- Shiva should not expose the raw default ruleset as-is
- the first integrated ruleset should be narrower and severity-mapped by Shiva
- parser failures and structural-invalidity findings should be treated separately from style/documentation findings

## Recommended Next Step
Before integrating `vacuum` into Shiva, define a first-party baseline ruleset that prioritizes:
- hard structural validity
- missing or conflicting operation identity
- broken examples and schema/example mismatches
- unresolved or invalid component use

De-prioritize by default:
- description duplication
- missing descriptions
- example completeness on every field
- naming-style rules that create large volumes of low-signal findings
