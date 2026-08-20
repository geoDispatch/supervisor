# Contributing to GeoDispatch

Thank you for contributing to GeoDispatch! Here's everything you need to get started.

---

## Setup

```bash
git remote add upstream https://github.com/geoDispatch/supervisor.git
```

---

## Workflow

```bash
# 1. create your branch
git checkout -b feature/your-feature-name

# 2. run tests before pushing
go test ./...

# 3. push and open a PR
git push origin feature/your-feature-name
```

---

## Commit Messages

```
feat: add device location caching
fix: resolve race condition in zone assignment
docs: update README with Docker setup
refactor: simplify haversine zone assignment
```

---

## Code Style

- Run `gofmt` before every commit
- Keep functions small and focused
- Comment exported functions
- Use the typed constants from `models` — never hardcode strings like `"red"` or `"AGENT_ERROR"`

---

## Pull Requests

- One feature or fix per PR
- Describe what you changed and why
- Include logs or screenshots if relevant
- Reference any related issues

---

## Iron Rules

- **Go calculates zones. AI decides actions. Never reversed.**
- All timestamps are Unix milliseconds (`int64`)
- All phones are E.164 format (`+212XXXXXXXXX`)
- Never commit `.env` or credentials

---

Questions? Open an issue or reach out to the team. 🚀