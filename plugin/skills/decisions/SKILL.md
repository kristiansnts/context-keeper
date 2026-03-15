---
name: decisions
description: View all architectural and implementation decisions saved for this project. Use when asked about past decisions, architecture choices, or why something was built a certain way.
---

# /decisions — View All Saved Decisions

Shows all architectural and implementation decisions saved for this project, with their rationale and current status.

## Usage
```
/decisions
/decisions [search term]
```

## What it does
Calls the `decisions` MCP tool to list all saved decisions from memory. Each decision shows:
- Title and category
- The decision made and why
- Whether it supersedes an older decision
- When it was saved

## Examples
- `/decisions` — show all decisions
- `/decisions auth` — filter decisions about authentication

## Instructions
When this skill is invoked, call the `decisions` MCP tool from the context-keeper server. If a search term is provided, pass it as the `search` argument. Format the output as a clean, readable list grouped by category.
