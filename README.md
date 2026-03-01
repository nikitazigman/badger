# badger

## Disclaimer
This project is a work in progress. Commands and output may change as the parser evolves.

## End Goal
Badger is a terminal tool for exploring SQLite files at the byte-level (header + b-tree page structures), mainly for learning and debugging SQLite internals.

## Inspiration
This project was inspired by the SQLite course on CodeCrafters, and it is an evolution of my original code written for that course.

## Requirements
- Go `1.25.0`

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
