#!/usr/bin/env node
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from '@modelcontextprotocol/sdk/types.js';
import { ContextKeeperStorage, resolveDbPath } from '@context-keeper/core';
import type { MemoryEntry } from '@context-keeper/core';
import path from 'path';

const PROJECT_ROOT = process.env.CONTEXT_KEEPER_ROOT || process.cwd();
const DB_PATH = resolveDbPath(PROJECT_ROOT);

const storage = new ContextKeeperStorage({ dbPath: DB_PATH, projectRoot: PROJECT_ROOT });

const server = new Server(
  { name: 'context-keeper', version: '0.1.0' },
  { capabilities: { tools: {} } },
);

server.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [
    {
      name: 'remember',
      description: 'Save important project knowledge to memory. Use this for architectural decisions, coding conventions, known gotchas, or any context that should persist across sessions.',
      inputSchema: {
        type: 'object',
        properties: {
          type: {
            type: 'string',
            enum: ['decision', 'convention', 'gotcha', 'context', 'note'],
            description: 'Type of memory: decision (architectural), convention (coding pattern), gotcha (known pitfall), context (background info), note (general)',
          },
          title: { type: 'string', description: 'Short descriptive title' },
          content: { type: 'string', description: 'Full content of the memory' },
          tags: {
            type: 'array',
            items: { type: 'string' },
            description: 'Optional tags for categorization',
          },
        },
        required: ['type', 'title', 'content'],
      },
    },
    {
      name: 'search',
      description: 'Search project memory using natural language. Returns relevant past decisions, conventions, and context.',
      inputSchema: {
        type: 'object',
        properties: {
          query: { type: 'string', description: 'Natural language search query' },
          limit: { type: 'number', description: 'Max results (default: 5)', default: 5 },
        },
        required: ['query'],
      },
    },
    {
      name: 'context',
      description: 'Get a summary of all project memory. Use at the start of a session to quickly load project context.',
      inputSchema: {
        type: 'object',
        properties: {
          type: {
            type: 'string',
            enum: ['decision', 'convention', 'gotcha', 'context', 'note', 'all'],
            description: 'Filter by type (default: all)',
            default: 'all',
          },
        },
      },
    },
    {
      name: 'decisions',
      description: 'List all architectural decisions recorded for this project.',
      inputSchema: { type: 'object', properties: {} },
    },
    {
      name: 'conventions',
      description: 'Get all coding conventions and patterns for this project.',
      inputSchema: { type: 'object', properties: {} },
    },
  ],
}));

server.setRequestHandler(CallToolRequestSchema, async (request) => {
  const { name, arguments: args } = request.params;

  try {
    switch (name) {
      case 'remember': {
        const entry = storage.add({
          type: (args?.type as MemoryEntry['type']) || 'note',
          title: String(args?.title || ''),
          content: String(args?.content || ''),
          tags: (args?.tags as string[]) || [],
        });
        return {
          content: [{
            type: 'text',
            text: `✅ Saved to memory: [${entry.type}] ${entry.title} (id: ${entry.id})`,
          }],
        };
      }

      case 'search': {
        const results = storage.search(String(args?.query || ''), Number(args?.limit) || 5);
        if (results.length === 0) {
          return { content: [{ type: 'text', text: 'No matching memory found.' }] };
        }
        const text = results.map(r =>
          `[${r.type.toUpperCase()}] ${r.title}\n${r.content}\n${r.tags.length ? `Tags: ${r.tags.join(', ')}` : ''}\n---`
        ).join('\n');
        return { content: [{ type: 'text', text }] };
      }

      case 'context': {
        const type = args?.type as string;
        const entries = type && type !== 'all'
          ? storage.list(100, type as MemoryEntry['type'])
          : storage.list(100);

        if (entries.length === 0) {
          return {
            content: [{ type: 'text', text: 'No project memory yet. Use the `remember` tool to save context.' }],
          };
        }

        const grouped = entries.reduce((acc, e) => {
          if (!acc[e.type]) acc[e.type] = [];
          acc[e.type].push(e);
          return acc;
        }, {} as Record<string, typeof entries>);

        const text = Object.entries(grouped).map(([type, items]) =>
          `## ${type.charAt(0).toUpperCase() + type.slice(1)}s\n\n` +
          items.map(e => `### ${e.title}\n${e.content}`).join('\n\n')
        ).join('\n\n');

        return { content: [{ type: 'text', text: `# Project Context\n\n${text}` }] };
      }

      case 'decisions': {
        const entries = storage.list(50, 'decision');
        if (entries.length === 0) {
          return { content: [{ type: 'text', text: 'No architectural decisions recorded yet.' }] };
        }
        const text = entries.map(e => `### ${e.title}\n${e.content}\n${e.tags.length ? `*Tags: ${e.tags.join(', ')}*` : ''}`).join('\n\n---\n\n');
        return { content: [{ type: 'text', text: `# Architectural Decisions\n\n${text}` }] };
      }

      case 'conventions': {
        const entries = storage.list(50, 'convention');
        if (entries.length === 0) {
          return { content: [{ type: 'text', text: 'No conventions recorded yet.' }] };
        }
        const text = entries.map(e => `### ${e.title}\n${e.content}`).join('\n\n---\n\n');
        return { content: [{ type: 'text', text: `# Coding Conventions\n\n${text}` }] };
      }

      default:
        return { content: [{ type: 'text', text: `Unknown tool: ${name}` }], isError: true };
    }
  } catch (error) {
    return {
      content: [{ type: 'text', text: `Error: ${error instanceof Error ? error.message : String(error)}` }],
      isError: true,
    };
  }
});

async function main() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  process.stderr.write('context-keeper MCP server running\n');
}

main().catch(err => {
  process.stderr.write(`Fatal: ${err}\n`);
  process.exit(1);
});
