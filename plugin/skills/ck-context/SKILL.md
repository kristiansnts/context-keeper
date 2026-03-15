---
name: ck-context
description: Load all saved project memory into the conversation — decisions, conventions, gotchas, and session summaries. Use when you need a full refresh of what's been saved for this project.
---

# /ck-context — Full Project Context Dump

Loads all saved memory for this project into the current conversation context.

## Usage
```
/context
```

## What it does
Calls the `context` MCP tool to retrieve everything saved:
- All active decisions
- All conventions
- All gotchas and warnings
- Recent session summaries

This is automatically called at session start via the SessionStart hook. Use `/context` manually if you want to refresh or remind Claude of the full project memory mid-conversation.

## Instructions
When this skill is invoked, call the `context` MCP tool from the context-keeper server. Display the output clearly organized by category. After showing it, confirm: "Project memory loaded — I'll keep these in mind for our session."
