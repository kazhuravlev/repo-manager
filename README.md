# Repo Manager

This tool is allow you to manage several repositories.

## Functionality

- [ ] Check that all local copy of repos is in the latest state
- [ ] Check that all repos has/has no specific dependencies
    - [ ] Golang
        - [x] Has some repo in `go.mod`
        - [x] Has concrete version in `go.mod`
        - [x] Has version which great or equal to concrete version in `go.mod`
        - [x] Has `latest` version. When this option is set - `repo-manager` will fetch module and detect the latest version.
