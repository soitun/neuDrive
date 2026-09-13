# `neudrive search`

Use this command when the user wants to search neuDrive content.

## Examples

- `neudrive search "migration"`
- `neudrive search "memory marker" memory`
- `neudrive search "project note" project/demo`
- `neudrive search "conversation note" /conversations`
- `neudrive search "imported config" /platforms`

## Notes

- A leading `/` is optional on the scope path.
- Without a scope, search covers all accessible file-tree roots, including conversations and imported archives. Custom roots can be scoped with an absolute path.
- `secret` search is not part of the public command surface.
