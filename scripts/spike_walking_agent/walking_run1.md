# Spike #149 Results

Model: `claude-sonnet-4-5-20250929`  Workdir: `/private/tmp/spike-target`  Sub-tasks: 5


## Per-mode totals

| Mode | Calls | Total cost | Total input billed | cache_read | cache_write | Output | Wallclock |
|---|---|---|---|---|---|---|---|
| walking | 5 | $1.0022 | 331,441 | 301,394 (90.9%) | 29,941 (9.0%) | 4,048 | 90.4s |

## Per-turn detail


### walking

| # | Sub-task | Duration | Input | cache_read | cache_write | Output | Cost |
|---|---|---|---|---|---|---|---|
| 1 | List all top-level files and directories in the current work | 31748ms | 34 | 77320 | 26575 | 1100 | $0.1395 |
| 2 | Read the file you saw named CLAUDE.md (or README.md if CLAUD | 14416ms | 18 | 53448 | 674 | 685 | $0.1683 |
| 3 | Search for any TODO or FIXME comments in *.go files. Report  | 14899ms | 18 | 55023 | 1017 | 824 | $0.2011 |
| 4 | Run `git log --oneline -5` and report the most recent commit | 11681ms | 18 | 57068 | 813 | 551 | $0.2296 |
| 5 | Cross-reference your previous answers: was the file you read | 17666ms | 18 | 58535 | 862 | 888 | $0.2637 |
