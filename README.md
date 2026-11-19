# stock-portfolio

Personal portfolio decision-support system: scores stocks on five factors, computes target weights, plans monthly contributions, and suggests slow trims. Trades are executed manually.

Spec: [`docs/spec.md`](docs/spec.md).

## Run analyses in your own Claude (MCP)

`stockportfolio mcp` is an MCP server over stdio. It lets Claude Code or Claude Desktop (on your subscription) read your portfolio data, filings and prices, test valuations, and submit analyses as drafts you review in the app. It opens the same SQLite database as the web server and works whether or not that is running. The app never edits Claude's configuration: you run the snippets below (the Settings page shows them with your paths filled in).

Build the binary first:

```sh
make build   # writes bin/stockportfolio
```

**Claude Code** (WSL), run once; replace `/abs/path/to/stock-portfolio` with the repository path:

```sh
claude mcp add stock-portfolio --scope user -- /abs/path/to/stock-portfolio/bin/stockportfolio mcp -config /abs/path/to/stock-portfolio/config.local.toml
```

`-config` makes the server work from any directory: a relative `db_path` in the config file is resolved against the config file's directory.

**Claude Desktop** (Windows): add this to `%APPDATA%\Claude\claude_desktop_config.json` (merge into any existing `mcpServers`) and restart Claude Desktop:

```json
{
  "mcpServers": {
    "stock-portfolio": {
      "command": "wsl.exe",
      "args": [
        "-e",
        "/abs/path/to/stock-portfolio/bin/stockportfolio",
        "mcp",
        "-config",
        "/abs/path/to/stock-portfolio/config.local.toml"
      ]
    }
  }
}
```

Then use the prompts `analyze_asset`, `research_ticker` and `portfolio_review` (Claude Code: `/mcp__stock-portfolio__analyze_asset WEGE3`; Desktop: the prompt menu). Drafts appear on the asset's AI analyst tab, labelled "via MCP"; new tickers appear under Candidates on the Watchlist page. See [`docs/spec.md`](docs/spec.md) §13.
