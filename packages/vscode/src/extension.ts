import * as vscode from 'vscode';
import path from 'path';
import { ContextKeeperStorage, resolveDbPath } from '@context-keeper/core';
import type { MemoryEntry } from '@context-keeper/core';

function getProjectRoot(): string | undefined {
  return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

function getStorage(projectRoot: string): ContextKeeperStorage {
  const dbPath = resolveDbPath(projectRoot);
  return new ContextKeeperStorage({ dbPath, projectRoot });
}

// ── Copilot Chat Participant (@ctx) ────────────────────────────────────────
async function handleChatRequest(
  request: vscode.ChatRequest,
  context: vscode.ChatContext,
  stream: vscode.ChatResponseStream,
  token: vscode.CancellationToken,
): Promise<vscode.ChatResult> {
  const projectRoot = getProjectRoot();
  if (!projectRoot) {
    stream.markdown('❌ No workspace open.');
    return {};
  }

  const storage = getStorage(projectRoot);
  const query = request.prompt.trim();

  try {
    if (!query || query.toLowerCase() === 'summary' || query.toLowerCase() === 'context') {
      const entries = storage.list(100);
      if (entries.length === 0) {
        stream.markdown('No project memory yet. Use `ctx add` in terminal or the **Context Keeper: Add Memory** command to start.');
        return {};
      }
      stream.markdown('# Project Context\n\n');
      const grouped = entries.reduce((acc, e) => {
        if (!acc[e.type]) acc[e.type] = [];
        acc[e.type].push(e);
        return acc;
      }, {} as Record<string, typeof entries>);
      for (const [type, items] of Object.entries(grouped)) {
        stream.markdown(`## ${type.charAt(0).toUpperCase() + type.slice(1)}s\n\n`);
        for (const item of items) {
          stream.markdown(`### ${item.title}\n${item.content}\n\n`);
        }
      }
    } else {
      const results = storage.search(query, 5);
      if (results.length === 0) {
        stream.markdown(`No results found for **"${query}"**.\n\nTry \`@ctx summary\` to see all project memory.`);
        return {};
      }
      stream.markdown(`## Results for "${query}"\n\n`);
      for (const r of results) {
        stream.markdown(`### [${r.type}] ${r.title}\n${r.content}\n\n`);
        if (r.tags.length) stream.markdown(`*Tags: ${r.tags.join(', ')}*\n\n`);
      }
    }
  } finally {
    storage.close();
  }

  return {};
}

// ── Extension Activation ───────────────────────────────────────────────────
export function activate(context: vscode.ExtensionContext): void {
  // Register Copilot Chat Participant
  if (typeof vscode.chat?.createChatParticipant === 'function') {
    const participant = vscode.chat.createChatParticipant('context-keeper.ctx', handleChatRequest);
    participant.iconPath = vscode.Uri.joinPath(context.extensionUri, 'assets', 'icon.png');
    context.subscriptions.push(participant);
  }

  // Command: Init
  context.subscriptions.push(vscode.commands.registerCommand('context-keeper.init', async () => {
    const projectRoot = getProjectRoot();
    if (!projectRoot) {
      vscode.window.showErrorMessage('No workspace open');
      return;
    }
    const storage = getStorage(projectRoot);
    storage.close();
    vscode.window.showInformationMessage('✅ context-keeper initialized! Run `ctx init` in terminal for full setup.');
  }));

  // Command: Add Memory
  context.subscriptions.push(vscode.commands.registerCommand('context-keeper.add', async () => {
    const projectRoot = getProjectRoot();
    if (!projectRoot) return;

    const type = await vscode.window.showQuickPick(
      ['decision', 'convention', 'gotcha', 'context', 'note'],
      { placeHolder: 'Memory type' },
    ) as MemoryEntry['type'] | undefined;
    if (!type) return;

    const title = await vscode.window.showInputBox({ prompt: 'Title', placeHolder: 'Short descriptive title' });
    if (!title) return;

    const content = await vscode.window.showInputBox({
      prompt: 'Content',
      placeHolder: 'Describe this memory...',
    });
    if (!content) return;

    const storage = getStorage(projectRoot);
    const entry = storage.add({ type, title, content, tags: [] });
    storage.close();

    vscode.window.showInformationMessage(`✅ Saved [${type}] ${entry.title}`);
  }));

  // Command: Search
  context.subscriptions.push(vscode.commands.registerCommand('context-keeper.search', async () => {
    const projectRoot = getProjectRoot();
    if (!projectRoot) return;

    const query = await vscode.window.showInputBox({ prompt: 'Search project memory', placeHolder: 'auth pattern, database choice...' });
    if (!query) return;

    const storage = getStorage(projectRoot);
    const results = storage.search(query, 5);
    storage.close();

    if (results.length === 0) {
      vscode.window.showInformationMessage(`No results for "${query}"`);
      return;
    }

    const items = results.map(r => ({ label: `[${r.type}] ${r.title}`, description: r.content.substring(0, 80), detail: r.tags.join(', ') }));
    const selected = await vscode.window.showQuickPick(items, { placeHolder: 'Select to copy to clipboard' });
    if (selected) {
      const result = results.find(r => r.title === selected.label.replace(/^\[.*?\] /, ''));
      if (result) await vscode.env.clipboard.writeText(`${result.title}\n\n${result.content}`);
    }
  }));

  // Command: Summary
  context.subscriptions.push(vscode.commands.registerCommand('context-keeper.summary', async () => {
    const projectRoot = getProjectRoot();
    if (!projectRoot) return;
    const storage = getStorage(projectRoot);
    const summary = storage.summary();
    storage.close();
    vscode.window.showInformationMessage(summary);
  }));
}

export function deactivate(): void {
  // nothing to clean up
}
