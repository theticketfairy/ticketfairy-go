# Contributing

Standing rules for people and coding agents that change this repository. They add to the
rules in `AGENTS.md` where that file exists.

## Clean up local worktrees after merge

When the work in a local git worktree is pushed to GitHub and its pull request is merged,
remove that worktree and its local branch. Do this in the same session that sees the merge.

1. Make sure that nothing is lost. `git status` must show no uncommitted changes, and
   `git log @{u}..` must show no unpushed commits. If you find either, or the branch has no
   upstream (then `git log @{u}..` fails), stop and keep the worktree.
2. From another checkout, run `git worktree remove <path>`. Do not add `--force`: it discards
   uncommitted work.
3. Confirm on GitHub that the pull request is merged. A successful `git branch -d` does not
   prove this. Then delete the local branch with `git branch -d <branch>`. A squash or rebase
   merge makes `-d` refuse. Use `git branch -D <branch>` only when the branch has no commits
   after the head commit of the merged pull request.
4. Run `git worktree prune` to remove records of worktree folders that were already deleted.

Do not remove the main checkout. Do not remove a worktree that another session or person still
uses, or a worktree whose pull request is still open or was closed without a merge.
