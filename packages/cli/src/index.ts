#!/usr/bin/env node
import { Command } from 'commander';
import chalk from 'chalk';
import path from 'path';
import fs from 'fs';
import { ContextKeeperStorage, resolveDbPath } from '@context-keeper/core';
import type { MemoryEntry } from '@context-keeper/core';

const program = new Command();

function getStorage(): ContextKeeperStorage {
  const projectRoot = process.cwd();
  const dbPath = resolveDbPath(projectRoot);
  return new ContextKeeperStorage({ dbPath, projectRoot });
}

function ensureInitialized(): void {
  const contextDir = path.join(process.cwd(), '.context');
  if (!fs.existsSync(contextDir)) {
    console.error(chalk.red('❌ Not initialized. Run `ctx init` first.'));
    process.exit(1);
  }
}

function typeColor(type: MemoryEntry['type']): string {
  const colors: Record<MemoryEntry['type'], (s: string) => string> = {
    decision: chalk.blue,
    convention: chalk.green,
    gotcha: chalk.yellow,
    context: chalk.cyan,
    note: chalk.white,
  };
  return (colors[type] || chalk.white)(type);
}

program
  .name('ctx')
  .description('Universal AI memory layer for your project')
  .version('0.1.0');

// ── init ────────────────────────────────────────────────────────────────────
program
  .command('init')
  .description('Initialize context-keeper in current project')
  .action(() => {
    const contextDir = path.join(process.cwd(), '.context');
    const gitignorePath = path.join(process.cwd(), '.gitignore');

    if (fs.existsSync(contextDir)) {
      console.log(chalk.yellow('⚠️  context-keeper already initialized.'));
      return;
    }

    fs.mkdirSync(contextDir, { recursive: true });

    // Create starter markdown files (these ARE committed to git)
    const files = {
      'decisions.md': `# Architecture Decisions\n\nRecord important architectural decisions here.\n\n## Example\n\n**Decision**: Use PostgreSQL over MySQL\n**Reason**: Better JSON support and full-text search\n**Date**: ${new Date().toISOString().split('T')[0]}\n`,
      'conventions.md': `# Coding Conventions\n\nDocument team conventions and patterns here.\n\n## Example\n\n- All services follow the Repository pattern\n- Use camelCase for variables, PascalCase for classes\n`,
      'gotchas.md': `# Known Gotchas\n\nDocument known pitfalls and workarounds here.\n\n## Example\n\n**Gotcha**: Don't use X because Y (tried, failed on date Z)\n`,
    };

    for (const [filename, content] of Object.entries(files)) {
      fs.writeFileSync(path.join(contextDir, filename), content);
    }

    // Gitignore the DB file
    const gitignoreEntry = '\n# context-keeper (commit markdown files, not DB)\n.context/context.db\n.context/context.db-shm\n.context/context.db-wal\n';
    if (fs.existsSync(gitignorePath)) {
      const existing = fs.readFileSync(gitignorePath, 'utf-8');
      if (!existing.includes('.context/context.db')) {
        fs.appendFileSync(gitignorePath, gitignoreEntry);
      }
    } else {
      fs.writeFileSync(gitignorePath, gitignoreEntry);
    }

    // Initialize the DB
    const storage = getStorage();
    storage.close();

    console.log(chalk.green('✅ context-keeper initialized!'));
    console.log('');
    console.log(chalk.cyan('Next steps:'));
    console.log('  ' + chalk.bold('ctx add') + '               — add project memory interactively');
    console.log('  ' + chalk.bold('ctx search <query>') + '    — search your memory');
    console.log('  ' + chalk.bold('ctx list') + '              — list all memory entries');
    console.log('');
    console.log(chalk.dim('📂 .context/ created — commit the markdown files to share with your team'));
  });

// ── add ─────────────────────────────────────────────────────────────────────
program
  .command('add')
  .description('Add a memory entry')
  .option('-t, --type <type>', 'Type: decision|convention|gotcha|context|note', 'note')
  .option('-T, --title <title>', 'Title of the memory')
  .option('-c, --content <content>', 'Content of the memory')
  .option('--tags <tags>', 'Comma-separated tags')
  .action(async (options) => {
    ensureInitialized();

    // If title/content not provided via flags, use interactive prompts
    if (!options.title || !options.content) {
      const inquirer = await import('inquirer');
      const prompt = inquirer.default?.prompt || (inquirer as any).prompt;
      const answers = await prompt([
        {
          type: 'list',
          name: 'type',
          message: 'Memory type:',
          choices: ['decision', 'convention', 'gotcha', 'context', 'note'],
          default: options.type,
          when: !options.type || options.type === 'note',
        },
        {
          type: 'input',
          name: 'title',
          message: 'Title:',
          when: !options.title,
          validate: (v: string) => v.trim() ? true : 'Title required',
        },
        {
          type: 'editor',
          name: 'content',
          message: 'Content (opens editor):',
          when: !options.content,
          validate: (v: string) => v.trim() ? true : 'Content required',
        },
        {
          type: 'input',
          name: 'tags',
          message: 'Tags (comma-separated, optional):',
          when: !options.tags,
        },
      ]);

      Object.assign(options, answers);
    }

    const storage = getStorage();
    const entry = storage.add({
      type: options.type || 'note',
      title: options.title,
      content: options.content,
      tags: options.tags ? options.tags.split(',').map((t: string) => t.trim()) : [],
    });
    storage.close();

    console.log(chalk.green(`✅ Saved [${typeColor(entry.type)}] ${chalk.bold(entry.title)} (id: ${entry.id})`));
  });

// ── search ──────────────────────────────────────────────────────────────────
program
  .command('search <query>')
  .description('Search project memory')
  .option('-n, --limit <n>', 'Max results', '5')
  .action((query, options) => {
    ensureInitialized();
    const storage = getStorage();
    const results = storage.search(query, parseInt(options.limit));
    storage.close();

    if (results.length === 0) {
      console.log(chalk.yellow('No results found for: ' + query));
      return;
    }

    console.log(chalk.bold(`\n🔍 Results for "${query}":\n`));
    for (const r of results) {
      console.log(`${chalk.dim(`#${r.id}`)} [${typeColor(r.type)}] ${chalk.bold(r.title)}`);
      console.log(chalk.dim('  ' + r.content.split('\n')[0].substring(0, 120)));
      if (r.tags.length) console.log(chalk.dim('  🏷  ' + r.tags.join(', ')));
      console.log('');
    }
  });

// ── list ─────────────────────────────────────────────────────────────────────
program
  .command('list')
  .description('List all memory entries')
  .option('-t, --type <type>', 'Filter by type')
  .option('-n, --limit <n>', 'Max results', '20')
  .action((options) => {
    ensureInitialized();
    const storage = getStorage();
    const entries = storage.list(
      parseInt(options.limit),
      options.type as MemoryEntry['type'] | undefined,
    );
    storage.close();

    if (entries.length === 0) {
      console.log(chalk.yellow('No memory entries yet. Run `ctx add` to get started.'));
      return;
    }

    console.log(chalk.bold(`\n📚 Project Memory (${entries.length} entries):\n`));
    for (const e of entries) {
      const date = new Date(e.updated_at).toLocaleDateString();
      console.log(`${chalk.dim(`#${e.id}`)} [${typeColor(e.type)}] ${chalk.bold(e.title)} ${chalk.dim(date)}`);
      if (e.tags.length) console.log(chalk.dim('     🏷  ' + e.tags.join(', ')));
    }
    console.log('');
  });

// ── summary ──────────────────────────────────────────────────────────────────
program
  .command('summary')
  .description('Show project memory summary')
  .action(() => {
    ensureInitialized();
    const storage = getStorage();
    console.log('\n' + storage.summary());
    storage.close();
  });

// ── delete ───────────────────────────────────────────────────────────────────
program
  .command('delete <id>')
  .description('Delete a memory entry by ID')
  .action((id) => {
    ensureInitialized();
    const storage = getStorage();
    const deleted = storage.delete(parseInt(id));
    storage.close();

    if (deleted) {
      console.log(chalk.green(`✅ Deleted entry #${id}`));
    } else {
      console.log(chalk.red(`❌ Entry #${id} not found`));
    }
  });

program.parse();
