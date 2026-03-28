# badger

## Disclaimer
This project is a work in progress. Commands and output may change as the parser evolves.

## End Goal
Badger is a terminal tool for exploring SQLite files at the byte level, primarily as a learning tool for understanding how SQLite stores data on disk.

The current CLI exposes parser-oriented commands. The intended product direction is an interactive TUI for exploring database metadata, pages, cells, and records.

## Inspiration
This project was inspired by the SQLite course on CodeCrafters, and it is an evolution of my original code written for that course.

## Requirements
- Go `1.26.1`

## Quick Start
```bash
make build
./bin/badger help
```

## CLI Usage
```text
badger inspect <file.db>
badger page <file.db> <N>
badger help
```

## Examples
```bash
./bin/badger inspect fixtures/sample.db
./bin/badger page fixtures/companies.db 2
```

## Tests
```bash
make test
```

## Fixtures
Sample databases for local testing are in `fixtures/`.
