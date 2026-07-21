# Back-Orbit Test Lab

The lab contains reproducible, realistic Docker Compose projects used to test
discovery, analysis, backup, restore, and drift detection. Template definitions
are versioned; running projects, generated credentials, and application data
live under `runtime/` and are intentionally excluded from Git.

## Materialise a project

```sh
./lab/scripts/materialize.sh 01-wordpress-mariadb
docker compose -f lab/runtime/projects/01-wordpress-mariadb/compose.yml up -d
```

Register the resulting absolute project path in Back-Orbit. When Back-Orbit is
itself containerised, mount `lab/runtime/projects` into the container at the
same absolute path so Compose labels and readable paths agree.

Never reuse generated lab credentials outside this local test environment.

## Lifecycle

1. Materialise and start a scenario.
2. Seed known application data.
3. Analyze and confirm the proposed protection blueprint.
4. Create and verify a snapshot.
5. Mutate or remove known data.
6. Restore and run the scenario verification.

`catalog.yaml` is the delivery contract for the first wave. A scenario marked
`ready` has a Compose template in this repository. `planned` means its
protection template exists, but its executable lab scenario is not yet ready.
Fifteen of the twenty-five scenarios are currently ready.
