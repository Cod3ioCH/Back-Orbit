# ADR-0014: Replaying a database export into the running service

Status: accepted
Date: 2026-07-21

## Context

Back-Orbit could export a database and show the command that would put it
back, but it could not run that command. A backup nobody has ever restored is
a backup nobody knows they have: the first real restore then happens under
pressure, by hand, at the worst possible moment.

Restoring a database is different from every other restore Back-Orbit
performs. Extracting a snapshot writes into an empty working directory and
cannot damage anything. Replaying an export writes into a database somebody is
using, and the write is not reversible.

## Decision

`POST /api/v1/restores/database` replays one service's export into the running
container it was taken from, as restore mode `database`.

**The confirmation is enforced on the server.** The request must repeat the
service name in a `confirm` field, and `restore.RestoreDatabase` rejects the
call when it does not match. Enforcing this only in the dialog would make it a
piece of interface decoration: the endpoint is reachable without it. Naming the
target is something no accidental request does.

**Only exports are replayable.** A database captured as `files_only` or
`consistent` has no command that puts it back, so the action is not offered for
it — neither in the API nor in the UI. Offering it would promise a restore this
path cannot perform.

**Credentials are read one key at a time** from the target container's own
environment, and only the keys that engine uses. PostgreSQL gets none: the load
runs inside the container over its local socket, where the server trusts its
own operating-system user. Nothing is passed on argv; MySQL's password travels
in `MYSQL_PWD`, MongoDB's in a config file on stdin.

**The snapshot is opened for the export alone.** `Include: */<dump file>` pulls
out the dump and leaves the staged copy of the data directory in the
repository. Restoring both would put a file-level copy of the data directory
on top of a server that is running on it.

## Consequences

### PostgreSQL: the exit code is not the verdict

`pg_dumpall --clean` drops everything before recreating it, including three
things PostgreSQL will not let anyone drop: `template1`, the database the
session is connected to, and the role the session is connected as. Each failed
`DROP` is followed by a `CREATE` that fails because the object is still there.

Running that with `ON_ERROR_STOP=1` aborts *after* the user databases have
already been dropped. The first live test of this code did exactly that, and
destroyed the database it had been asked to bring back.

So the load runs with `ON_ERROR_STOP=0` and the outcome is measured instead:
the tables are counted afterwards, and a load that leaves nothing behind is
reported as the failure it is. The handful of messages that appear on every
healthy restore are filtered from the warnings by name — a warning that shows
up every time teaches people to ignore warnings. Everything else is reported.

`psql` connects with `-d postgres`, because without it psql connects to a
database named after the user, which need not exist.

### MySQL and MariaDB: `--add-drop-database` on the export

Without it, replaying restores the tables the dump holds and leaves everything
else in place — a table created after the backup survives, and the result is a
database that never existed at any point in time. The flag makes the dump drop
and recreate each database it contains, so a restore is a replacement.

### MongoDB: the archive is uploaded, not streamed

`mongorestore` reads its archive from standard input, which leaves nowhere to
hand it a password — that also has to arrive on stdin. Only one of them can
have it, so the archive goes in as a file at `/tmp/back-orbit-restore.archive`
and the credentials keep the pipe. The file is removed on every path.

`--drop` drops the collections the archive holds and nothing else, so a
collection created after the backup survives a MongoDB restore. This is a
property of the tool, and the UI says so rather than claiming a full
replacement.

`admin.*` and `config.*` are excluded, matching the replay command. Restoring
the admin database replaces the target server's accounts with the ones from the
machine the backup was taken on — verified, and the reason this is not a plain
restore.

### Cancellation

Cancelling before the load reports "cancelled before the database was touched".
Cancelling during it reports that the database may hold a partial restore,
because it may. Neither claims more than it knows.
