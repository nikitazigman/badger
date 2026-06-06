# badger

## Disclaimer
This project is a work in progress. Commands and output may change as the parser evolves.

## End Goal
Badger is a terminal tool for exploring SQLite files at the byte level, primarily as a learning tool for understanding how SQLite stores data on disk.

Badger opens directly into an interactive TUI for exploring database metadata, pages, cells, and records.

## Inspiration
This project was inspired by the SQLite course on CodeCrafters, and it is an evolution of my original code written for that course.

## Requirements
- Go `1.26.1`

## Quick Start
```bash
make build
./bin/badger fixtures/sample.db
```

## Usage
```text
badger <file.db>
```

## Examples
```bash
./bin/badger fixtures/sample.db
./bin/badger fixtures/companies.db
```

## Tests
```bash
make test
```

## Fixtures
Sample databases for local testing are in `fixtures/`.
