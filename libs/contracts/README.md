# Shared Contracts

`contracts.json` is the single source of truth for cross-service names (queues, exchanges,
buckets, Redis keys, message field lists). Each service reads these values at build/runtime
or copies the constants into its own language. If you change a name here, change it in every
service. Human-readable detail lives in `../../docs/contracts.md`.
