# Gateway Agent Notes

- **Interfaces** live in `config.go` (`ObjStore`, `Store`, `Broker`, `Cache`).
- **Handlers** are pure and unit-tested with fake implementations — see `handlers_test.go`.
- **Real adapters** are in `store.go` (pgxpool), `objstore.go` (minio-go), `broker.go` (amqp091-go), `cache.go` (go-redis).
- **Integration tests** are behind `-tags integration` and require Docker (testcontainers).
- **Never** call other services directly — only interact with broker/DB/cache/object-store per `docs/contracts.md`.
- **Queue** name is `process` (constant `workQueue` in `config.go`).
- **Key format**: Redis `job:{id}`, MinIO originals `originals/<id>.<ext>`, processed `processed/<id>.png`.
