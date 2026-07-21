# ADR-0013: Database dumps run inside the database's own container

## Status

Accepted. Refines ADR-0006 for logical database exports.

## Context

ADR-0006 decided that database dumps run in short-lived Back-Orbit helper containers rather than through `docker exec` in the target container, reasoning that an arbitrary image cannot be trusted to carry the dump tools.

That reasoning holds for the general case but is dominated by a stronger constraint for the engines Back-Orbit exports. `pg_dump` and `pg_dumpall` refuse to dump a server newer than themselves — `pg_dump 16` against a PostgreSQL 17 server exits with a version error. A helper container therefore has to carry the exact version of every server it might meet, discovered at backup time and pulled on demand. It fails precisely when the backup is needed, and it fails after the operator has been told the database is protected.

The binaries sitting beside a running server always match it.

## Decision

Logical dumps run inside the database's own container, through the Docker exec API, using the tools that shipped with the server.

The command is an argument vector, never a shell string. No password is passed: the dump runs over the container's local socket, where the server trusts its own operating-system user. Only one value is read from the container's environment — the database user name — through an API that returns a single key, because a function handing back the whole environment invites a caller to keep the password with it.

Output is streamed to disk rather than buffered: a dump is exactly as large as the database. Docker's stream framing is demultiplexed, so error text cannot be interleaved into the dump — which would produce a corrupt file that still looks like SQL.

Dumps are written into the staged tree and travel inside the same snapshot as the file copies. A dump kept apart from the volume it was taken from is a second thing to find, keep and match up at the worst possible moment.

A failed dump does not fail the backup. The file copy underneath is still worth having, and the run carries a warning saying the export did not happen.

Volume staging keeps using inert helper containers as ADR-0006 decided. Nothing is executed there, and nothing about that path needed to change.

## What is dumped

PostgreSQL is exported with `pg_dumpall`, which includes roles: ownership and grants live outside any single database, and tables restored into a server with no users to own them are not a restore of the system anyone had.

The MySQL family is exported per user database, with the system schemas excluded by name. `--all-databases` was the obvious choice and produced an unrestorable dump: it includes the `mysql` schema, whose user table carries the source server's credentials. Replaying that into a running server replaces its accounts mid-import, the importing session loses the authorisation it is using, and the import dies — before reaching any user database, because `mysql` sorts ahead of most names. The dump was several megabytes and restored nothing.

MongoDB is exported as a single `--archive` stream. Its credentials are the awkward case: mongodump has no environment variable for a password, and the documented ways in are the command line, a config file, or a prompt. The command line is readable by any process on the host through `ps`, and a config file written into the container would leave the password on disk if anything died before removing it — so the config goes in on standard input and exists only in the pipe between the two processes. An unauthenticated server, which is the image's default, needs none of this.

A mongodump archive contains the `admin` database, and mongodump has no `--excludeDatabase`. Restoring the archive therefore replaces the target server's accounts with the source's: verified against real servers, the documents came back and then nothing could authenticate except the credentials from the machine the backup was taken on. The exclusion belongs on the way back in, so the replay command carries `--nsExclude 'admin.*'`.

The engines therefore differ on purpose. `pg_dumpall` emits roles as statements a target can absorb; MySQL replaces a system catalogue wholesale. Grants are consequently not carried for the MySQL family, and the target server keeps its own accounts.

## Consequences

- Version mismatch, the most common practical failure of external dump tooling, cannot occur.
- Back-Orbit needs the Docker exec capability, which is a larger privilege than creating an inert container. It already creates and runs containers, so this widens an existing capability rather than adding a new class of one.
- An image that runs a database server without its client tools cannot be dumped this way. The run reports that rather than silently falling back to a file copy.
- Engines are added to the export path one at a time, and only once they can actually be exported. Until then they keep the warning that their files were copied rather than dumped.
