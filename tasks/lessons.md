# Lessons

- Use SQLite `synchronous=FULL` for this collector: preserving database consistency is distinct from preserving acknowledged commits after power loss.
- Isolate configuration tests from host environment variables and the machine's default configuration file.
- Keep the documented minimum Go version and CI matrix aligned with `go.mod` and dependency requirements.
