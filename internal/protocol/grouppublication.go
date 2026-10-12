package protocol

import "errors"

// GroupPublication binds an append to its local invitation, or to a governance
// operation. The authenticated HTTP signature covers this entire body. The
// original GroupCommit stays byte-for-byte recoverable, including legacy ones.
type GroupPublication struct {
	V          int         `json:"v"`
	Commit     GroupCommit `json:"commit"`
	Invitation *string     `json:"invitation"`
}

func (p GroupPublication) Validate() error {
	if p.V != 1 || p.Invitation == nil || *p.Invitation != "" && !ValidHash(*p.Invitation) {
		return errors.New("group: explicit publication binding required")
	}
	return p.Commit.Validate()
}

// GroupInvitationCancel is an exact-ID fence, not a membership change. Seq/Prev
// name the proposal's original publication slot; Head is the current head the
// cancelling human verified. A successful replay need not still have that head.
type GroupInvitationCancel struct {
	V          int    `json:"v"`
	Realm      string `json:"realm"`
	Conv       string `json:"conv"`
	Bootstrap  string `json:"bootstrap"`
	Person     string `json:"person"`
	Invitation string `json:"invitation"`
	Seq        int64  `json:"seq"`
	Prev       string `json:"prev"`
	Head       string `json:"head"`
}

func (c GroupInvitationCancel) Validate() error {
	if c.V != 1 || !ValidID(c.Realm) || !ValidHash(c.Conv) || !ValidFingerprint(c.Bootstrap) || !ValidID(c.Person) || !ValidHash(c.Invitation) || c.Seq < 1 || !ValidHash(c.Prev) || !ValidHash(c.Head) {
		return errors.New("group: invalid exact invitation cancellation")
	}
	return nil
}
