---
title: "Ground Control as a Harbor 3 Extension"
weight: 5
description: "Plan item 9.5 (ADR-0019) lets Ground Control react to registry events, for example to move a group's artifact when its tag is pushed again, so groups follow tags without an operator syncing them."
source: "docs/guides/harbor3-extension.md"
---

Plan item 9.5 (ADR-0019) lets Ground Control react to registry events, for
example to move a group's artifact when its tag is pushed again, so groups
follow tags without an operator syncing them.

## What exists

- `fleet.RegistryEventSource`: the port a registry event feed implements.
  `Subscribe` hands each event to a handler in order.
- `fleet.GroupService.FollowPush`: for an `ArtifactPushed` event it updates
  every group that lists the pushed repository and tag under another
  digest, in one transaction with a `GroupChanged` event per group, so the
  async plane republishes the group state and the satellites replicate
  the new digest. Pushes of untagged artifacts, of tags no group lists and
  of the digest a group already has change nothing.
- `async.RegistryFollower`: the job that subscribes to a source and calls
  `FollowPush`. A failed event is logged and the subscription continues; the
  next push of the tag repairs the group.

## The gap

Harbor 3 has not published its extension API and event feed, so there is
no adapter that implements `RegistryEventSource`, and neither `serve` nor
`worker` runs a `RegistryFollower`. Groups change only through
`POST /api/groups/sync`, as before.

When the API exists, the remaining work is an adapter under
`internal/groundcontrol/adapters/harbor3` that implements
`RegistryEventSource` (and, if Harbor 3 changes them, the robot and
artifact calls of `fleet.Registry`), composing the follower in the worker
when the adapter is configured, and an end-to-end test that pushes a tag
and sees a satellite replicate the new digest.
