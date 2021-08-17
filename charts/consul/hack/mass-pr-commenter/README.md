# Mass PR Commenter Script

This script allows the user to comment on every open pull request in a given repository.

## Prerequisites

- [Python](https://www.python.org/)
  ```bash
  brew install python
  ```
- [GitHub CLI](https://cli.github.com/)
  ```bash
  brew install gh
  ```

## Usage

```bash
python3 comment.py OWNER/REPO ./comment.md
```

The first argument is the repository whose open pull requests will be commented on. The second argument is the path to a markdown file whose contents will be posted as the comment on each pull request.

The script will collect the number ids of each open pull request and prompt you for confirmation before creating the comments.

```bash
Comment on 19 pull requests to the hashicorp/consul-helm repository with the contents of ./comment.md? [y/N]
```

