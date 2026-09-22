# Orca Label Namespace

Orca reads Docker labels to learn which project a resource belongs to and how it
should be treated. Explicit labels are the preferred source of this
information. Anything Orca infers later (Compose project labels, names,
networks) always ranks **lower in confidence** than an explicit `com.orca.*`
label.

Constants: `internal/shared/labels/labels.go`.

## Labels

| Label | Value | Example | Meaning |
|---|---|---|---|
| `com.orca.project` | project name | `medical-ai` | Logical project the resource belongs to. |
| `com.orca.environment` | stage | `development` | Deployment stage: `development`, `staging`, `production`… |
| `com.orca.owner` | person or team | `ml-platform` | Who is responsible for the resource. |
| `com.orca.retention` | duration | `30d` | How long an unused resource should be kept. |
| `orca.gc.protected` | `true` | `true` | Marks the resource as a GC root: never collected. |
| `orca.gc.persistent` | `true` | `true` | The volume holds data that must be kept (protected). |
| `orca.gc.temporary` | `true` | `true` | The volume is disposable once no container uses it. |

Labels apply to images, containers and volumes. The `orca.gc.*` labels control
garbage collection. They deliberately sit outside `com.orca.` so they are short
to type: `docker volume create --label orca.gc.protected=true models`.

### Values fail safe

- **Protective labels** (`orca.gc.protected`, `orca.gc.persistent`): any value
  except an explicit false (`false`, `0`, `f`) counts as set. A typo such as
  `yes` still protects.
- **`orca.gc.temporary`** makes data disposable, so only an explicit true value
  (`true`, `1`, `t`) counts. A typo does nothing.
- Protection always wins over `temporary`.

### What is enforced today

| Label | Effect (Sprint 1) |
|---|---|
| `orca.gc.protected` | protects volumes (see `docs/volumes.md`); marks images and containers `Protected`, which removes them from the reclaim estimates |
| `orca.gc.persistent` | protects volumes |
| `orca.gc.temporary` | makes an unused volume a `candidate` |
| `com.orca.retention` | any value protects the volume until the policy engine evaluates durations (Sprint 3) |
| `com.orca.project` | groups containers, images and volumes into projects (`orca graph`, `orca inventory`); can protect volumes through `volumes.protected_projects` |
| `com.orca.environment`, `com.orca.owner` | recorded only; project accounting is Sprint 4 |

When `com.orca.project` is absent, the Docker Compose project
(`com.docker.compose.project`) is used, with `Source: "compose"` (lower
confidence) instead of `"label"`.

## Example

A project described with explicit labels:

```text
Project: medical-ai
  Containers: medical-api, postgres, redis
  Images:     medical-api:dev, postgres:16, redis:7
  Volumes:    medical_postgres_data, medical_models
```

```yaml
# compose.yaml
services:
  api:
    image: medical-api:dev
    labels:
      com.orca.project: medical-ai
      com.orca.environment: development
      com.orca.owner: ml-platform
volumes:
  medical_models:
    labels:
      com.orca.project: medical-ai
      orca.gc.protected: "true"
```

Image labels can be set at build time:

```dockerfile
LABEL com.orca.project="medical-ai" com.orca.retention="30d"
```

Docker labels can't be changed on an existing container, image or volume.
Resources must be labelled when they are created.
