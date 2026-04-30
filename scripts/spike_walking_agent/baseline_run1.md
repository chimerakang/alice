# Spike #149 Results

Model: `claude-sonnet-4-5-20250929`  Workdir: `/private/tmp/spike-target`  Sub-tasks: 5


## Per-mode totals

| Mode | Calls | Total cost | Total input billed | cache_read | cache_write | Output | Wallclock |
|---|---|---|---|---|---|---|---|
| baseline | 5 | $0.4334 | 286,673 | 203,830 (71.1%) | 82,745 (28.9%) | 4,112 | 114.9s |

## Per-turn detail


### baseline

| # | Sub-task | Duration | Input | cache_read | cache_write | Output | Cost |
|---|---|---|---|---|---|---|---|
| 1 | List all top-level files and directories in the current work | 32257ms | 26 | 51565 | 26630 | 1037 | $0.1310 |
| 2 | Read the file you saw named CLAUDE.md (or README.md if CLAUD | 20204ms | 18 | 37852 | 13757 | 813 | $0.0752 |
| 3 | Search for any TODO or FIXME comments in *.go files. Report  | 15647ms | 18 | 37834 | 13723 | 545 | $0.0710 |
| 4 | Run `git log --oneline -5` and report the most recent commit | 23550ms | 18 | 38791 | 14775 | 862 | $0.0800 |
| 5 | Cross-reference your previous answers: was the file you read | 23287ms | 18 | 37788 | 13860 | 855 | $0.0762 |
