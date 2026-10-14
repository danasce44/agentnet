# Captured conversation audiences in v0.8.28

`cau1` identifies readers of a `HumanTurn` carrying a signed `scope` whose
role is empty and whose audience is `conversation`. It is independently
advertised; `rm1`, `mdl2`, and `tpc1` do not imply it. Such copies still
require `hgp1` and every existing base requirement, including `ha1` for
archive descriptors. Producers may advertise 28 names; readers accept 32.
The signed capability record remains bounded at 1024 bytes.

The proof uses the existing audience PID, invitation hash, decision hash,
public scope, and host acceptance. Canonical bytes and execution consent
are unchanged. Browser readers verify the same current signed person,
exact pinned host key, invitation, decision, topic, and captured audience
as native readers. Outputs additionally need the exact original request
and its captured requester authority. Helpers gain no conversation
membership or earlier ambient history.

Native and browser durable copies retain the independent requirement in
`required_cap` as an existing base followed by `+cau1`. Archive packing
carries it forward after child body erasure and expired-descriptor
repacking. Unsupported readers keep their original sealed copies waiting;
current signed support releases those copies without resealing. History
imports remain inert and restricted to existing own-device authority.

An extracted published v0.8.27 browser codec refuses the new native-sealed
proof. Current cross-codec, browser admission, missing-capability, exact
requester, and archive regressions cover the extension. Native context
selection, ordering, and recovery are handled separately in this release.
