"""Comment Script

This script fetches all PRs on a given GitHub repository and adds a comment

Expects 2 arguments, a repository as OWNER/REPOSITORY and the path to a text
file which will be the content of the comments.

```python
python3 comment.py OWNER/REPOSITORY ./comment.md
```
"""

from json import loads
from subprocess import PIPE, run
from sys import argv


def fetch(repo: str) -> bytes:
    # returns a byte array of JSON with the number ids of each PR on the given repo
    return run(
        ["gh", "pr", "list", "--json", '"number"', "-R", repo], stdout=PIPE
    ).stdout


def format(prs: bytes) -> list[int]:
    # returns the number ids of the PRs for the given bytes array of JSON
    return [pr["number"] for pr in loads(prs.decode("utf-8"))]


def comment(prs: list[int], repo: str, comment_file: str) -> str:
    # comments on each PR in the given list of ids on the given repo with the
    # contents of comment_file
    return "".join(
        run(
            ["gh", "pr", "comment", str(pr), "-F", comment_file, "-R", repo],
            stdout=PIPE,
        ).stdout.decode("utf-8")
        for pr in prs
    )


if __name__ == "__main__":
    repo = argv[1]
    comment_file = argv[2]
    prs = format(fetch(repo))
    response = input(
        f"Comment on {len(prs)} pull requests to the {repo} repository with the contents of {comment_file}? [y/N] "
    )
    if response in ("y", "Y"):
        comment(prs, repo, comment_file)
        print("Done")
