# AI Ant Farm

Run the server and open http://localhost:8080 to watch the simulation.

Install dependencies and run:

```bash
go mod tidy
go run main.go
```

The frontend connects via WebSocket to `/ws` and renders the grid.
