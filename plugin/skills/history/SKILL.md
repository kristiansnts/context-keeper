---
name: history
description: Show the full evolution history of a saved decision — what changed and why. Use when asked how a decision evolved or what approaches were tried before the current one.
---

# /history — Decision History & Evolution

Shows the full evolution history of a saved decision — what it was before, what changed, and why.

## Usage
```
/history [decision title or id]
```

## What it does
Calls the `history` MCP tool to walk the supersedes chain for a decision. Useful for understanding:
- Why a decision changed over time
- What approaches were tried and rejected
- The full reasoning trail

## Examples
- `/history database` — show history of the database decision
- `/history auth strategy` — show how auth approach evolved

## Instructions
When this skill is invoked, call the `search` MCP tool first to find the decision by title/keyword, then call the `history` MCP tool with the found decision's id. Display the chain chronologically from oldest to newest, showing what changed and why at each step.
