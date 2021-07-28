# Nomad Spotinst Elastigroup Autoscaler

The `spotinst-elastigroup` target plugin allows for the scaling of the Nomad cluster clients via creating and destroying Spotinst Elastigroup instances.

## Documentation

### Agent Configuration Options

To use the `spotinst-elastigroup` plugin, the agent configuration needs to be populated with the appropriate target block.  Required config properties are listed below.

```hcl
target "spotinst-elastigroup" {
  driver = "spotinst-elastigroup"
  config = {
    token = "local/token"
    account_id = "local/account_id"
  }
}
```

- `token` `(string: "")` - A Spotinst API Token.  Can use environment variable `SPOTINST_TOKEN` instead.
- `account_id` `(string: "")` - A Spotinst Account ID.  Can use environment variable `SPOTINST_ACCOUNT` instead.

### Policy Configuration Options

```hcl
check "hashistack-allocated-cpu" {
  # ...
  target "spotinst-elastigroup" {
    provider            = "azure"
    elastigroup_id      = "sg-123456"
  }
  # ...
}
```

- `provider` `(string: "")` - The cloud provider used by Spot.  Can be one of `aws`, `azure` or `gcp`.
- `elastigroup_id` `(string: "")` - The elastigroup ID.
