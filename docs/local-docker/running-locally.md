# Running everything locally

This configuration establishes an exact architectural mirror of a 
production workspace. It provisions a PostgreSQL container with the 
pgvector engine, mounts automated initialization scripts, prepares a 
Valkey container instance, and boots a sidecar execution task to 
handle asset preloading without manual initialization.

##  Get the files
The needed files are
1. init-extensions.sql
2. docker-compose.yaml

## Run the thing

```bash
docker compose up -d
```

## Startup logic
### Database Bootstrapping
The core database server instantiates a default schema namespace 
(ai-postgres) owned by the ai user descriptor. 
It looks into /docker-entrypoint-initdb.d/ and processes
init-extensions.sql, binding rights and initializing 
the spatial index type vector.

### Model Hydration
The ollama-bootstrap script evaluates structural loop probes 
against the main AI server engine. Once responsive, 
it fetches the required foundational weights 
(llama3 and nomic-embed-text) over the network, mapping them to 
standard shared local disk volumes.

### Application Synchronization
The Go application container is blocked from running until
its underlying system network components are verified healthy.
Once fully live, it maps itself to port 8080.

## Open the app

`http://localhost:8080/`

## Tear down

```bash
docker compose down -v
```