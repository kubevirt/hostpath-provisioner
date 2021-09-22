#!/usr/bin/env bash

set -exuo pipefail

function cleanup_gh_install() {
    [ -n "${gh_cli_dir}" ] && [ -d "${gh_cli_dir}" ] && rm -rf "${gh_cli_dir:?}/"
}

function ensure_gh_cli_installed() {
    if command -V gh; then
        return
    fi

    trap 'cleanup_gh_install' EXIT SIGINT SIGTERM

    # install gh cli for uploading release artifacts, with prompt disabled to enforce non-interactive mode
    gh_cli_dir=$(mktemp -d)
    (
        cd  "$gh_cli_dir/"
        curl -sSL "https://github.com/cli/cli/releases/download/v${GH_CLI_VERSION}/gh_${GH_CLI_VERSION}_linux_amd64.tar.gz" -o "gh_${GH_CLI_VERSION}_linux_amd64.tar.gz"
        tar xvf "gh_${GH_CLI_VERSION}_linux_amd64.tar.gz"
    )
    export PATH="$gh_cli_dir/gh_${GH_CLI_VERSION}_linux_amd64/bin:$PATH"
    if ! command -V gh; then
        echo "gh cli not installed successfully"
        exit 1
    fi
    gh config set prompt disabled
}

function update_github_release() {
    # note: for testing purposes we set the target repository, gh cli seems to always automatically choose the
    # upstream repository automatically, even when you are in a fork

    set +e
    if ! gh release view --repo "$GITHUB_REPOSITORY" "$TAG" ; then
        set -e
        gh release create --repo "$GITHUB_REPOSITORY" "$TAG" --title="$TAG"
    else
        set -e
    fi

    gh release upload --repo "$GITHUB_REPOSITORY" --clobber "$TAG" \
        deploy/*.yaml
}

function main() {
    TAG="$(git tag --points-at HEAD | head -1)"
    if [ -z "$TAG" ]; then
        echo "commit $(git show -s --format=%h) doesn't have a tag, exiting..."
        exit 0
    fi

    export TAG

    GIT_ASKPASS="$(pwd)/automation/git-askpass.sh"
    [ -f "$GIT_ASKPASS" ] || exit 1
    export GIT_ASKPASS

    ensure_gh_cli_installed

    gh auth login --with-token <"$GITHUB_TOKEN_PATH"

    update_github_release
}

main "$@"

