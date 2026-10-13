package client

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/misunders2d/agentnet/internal/envelope"
	"github.com/misunders2d/agentnet/internal/protocol"
)

// Report bodies and their existing notified flags are the durable coverage
// record. A snapshot is not a new request. Neither these flags nor an alert
// changes review/decision authority or NoticeReport's exact-report Result.
type reviewTransition struct {
	Host, HostKey, ID, Key, State string
	Attempt                       int64
	NativeToken                   string
}

type storedReviewReport struct {
	ID, Host, Key, State string
	Notified             bool
	Report               Report
}

type remoteReviewAlerts struct {
	Counts  map[string]int
	Fresh   []string
	Covered []string
	Stalled bool
}

func reportPartition(host, key string) string { return host + "\x00" + key }
func reportTransition(r storedReviewReport, it ReportItem) reviewTransition {
	state := it.State
	if it.State == stateRunning {
		state += ":" + it.Blocker
	}
	transition := reviewTransition{Host: r.Host, HostKey: r.Key, ID: it.ID, Key: it.Key, State: state, Attempt: it.Attempt}
	if it.Native != nil {
		transition.NativeToken = it.Native.Token
	}
	return transition
}

func (a *Agent) nativeReviewEligible(r storedReviewReport, it ReportItem) bool {
	n := it.Native
	if n == nil || n.Harness != "codex" || n.Host != r.Host || n.Request != it.ID || n.Key != it.Key || n.Attempt != it.Attempt || it.Attempt < 1 || it.State != stateRunning || !protocol.ValidID(n.Token) || n.Thread == "" || n.Turn == "" || n.Expires < 0 || n.Expires != 0 && n.Expires <= time.Now().UnixMilli() {
		return false
	}
	if valid, e := a.nativeOwnerHumanIn(a.store.db, a.Address, a.Self().Fingerprint()); e != nil || !valid {
		return false
	}
	owner, e := ownerReportHolds(a.store.db, r.Host, r.Key)
	if e != nil || !owner {
		return false
	}
	revoked, e := revokedDeviceKnown(a.store.db, r.Host, r.Key)
	return e == nil && !revoked
}

func notificationReport(body, host, key string) (Report, bool) {
	r, ok := ParseReport(body)
	if !ok || r.At <= 0 || !protocol.ValidFingerprint(key) {
		return Report{}, false
	}
	if _, _, err := protocol.SplitAddress(host); err != nil {
		return Report{}, false
	}
	if r.Host != "" && r.Host != host {
		return Report{}, false
	}
	r.Host = host // authenticated sender, never a body-supplied attribution
	seen := map[string]bool{}
	for i := range r.Items {
		it := &r.Items[i]
		if !protocol.ValidID(it.ID) || !protocol.ValidFingerprint(it.Key) || it.Attempt < 0 || it.Since <= 0 || seen[it.ID] {
			return Report{}, false
		}
		if _, _, err := protocol.SplitAddress(it.From); err != nil {
			return Report{}, false
		}
		if it.Kind != envelope.KindTask && it.Kind != envelope.KindQuestion && it.Kind != envelope.KindMessage {
			return Report{}, false
		}
		if it.State != stateAwaiting && it.State != stateHeld && it.State != stateNeedHuman && it.State != stateInterrupt && it.State != stateRunning { // alertReviewStates
			return Report{}, false
		}
		if it.Blocker != blockerOf(it.Kind, it.State) && !(it.State == stateRunning && it.Blocker == BlockerStalled) {
			return Report{}, false
		}
		seen[it.ID] = true
		it.Result = nil // never treat a result supplied in a report body as proof
	}
	return r, true
}

func (a *Agent) remoteReviewAlerts() (remoteReviewAlerts, error) {
	out := remoteReviewAlerts{Counts: map[string]int{}}
	rows, err := a.store.db.Query(`SELECT id, sender, coalesce(verified_by, ''), body, notified, state
  FROM inbox WHERE conv IS NULL AND local = 0 AND replica = 0 AND (`+receivedNotice+`) AND NOT `+revokedInboxSender+` ORDER BY rowid`, envelope.KindMessage, envelope.StatusReviewNotice)
	if err != nil {
		return out, err
	}
	reports := map[string]storedReviewReport{}
	latest := map[string]storedReviewReport{}
	previous := map[string]storedReviewReport{}
	for rows.Next() {
		var r storedReviewReport
		var body string
		if err := rows.Scan(&r.ID, &r.Host, &r.Key, &body, &r.Notified, &r.State); err != nil {
			rows.Close()
			return out, err
		}
		parsed, ok := notificationReport(body, r.Host, r.Key)
		if !ok {
			continue
		}
		r.Report = parsed
		reports[r.ID] = r
		partition := reportPartition(r.Host, r.Key)
		latest[partition] = r
		if r.Notified {
			previous[partition] = r
		} else {
			out.Covered = append(out.Covered, r.ID)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	settled, err := a.settledReviewTransitions(reports)
	if err != nil {
		return out, err
	}
	for partition, r := range latest {
		if r.State != stateNeedHuman {
			continue
		}
		covered := map[reviewTransition]bool{}
		for _, it := range previous[partition].Report.Items {
			if it.Actionable || a.nativeReviewEligible(previous[partition], it) {
				covered[reportTransition(previous[partition], it)] = true
			}
		}
		for _, it := range r.Report.Items {
			transition := reportTransition(r, it)
			native := a.nativeReviewEligible(r, it)
			if !it.Actionable && !native || settled[transition] || it.State == stateRunning && it.Blocker != BlockerStalled && !native {
				continue
			}
			out.Counts[r.Host]++
			if !covered[transition] {
				if it.State == stateRunning && it.Blocker == BlockerStalled {
					out.Stalled = true
				}
				raw, _ := json.Marshal(transition)
				out.Fresh = append(out.Fresh, "remote\x00"+string(raw))
			}
		}
	}
	slices.Sort(out.Fresh)
	return out, nil
}

// This notification-only projection follows a proven decision result back to
// its original authenticated report. It does not copy Result onto a later
// report or authorize any action. Unknown/refused correlations stay open.
func (a *Agent) settledReviewTransitions(reports map[string]storedReviewReport) (map[reviewTransition]bool, error) {
	out := map[reviewTransition]bool{}
	// The store uses one SQLite connection. Resolve role eligibility before
	// opening result rows; querying it inside that cursor would deadlock.
	nativeEligibility := map[reviewTransition]bool{}
	for _, r := range reports {
		for _, it := range r.Report.Items {
			if it.Native != nil {
				nativeEligibility[reportTransition(r, it)] = a.nativeReviewEligible(r, it)
			}
		}
	}
	rows, err := a.store.db.Query(`SELECT s.sender, coalesce(s.verified_by,''), s.ref_id, s.ref_fp, s.body, d.id, d.body
  FROM inbox s JOIN outbox d ON d.id = CASE WHEN json_valid(s.body) THEN json_extract(s.body,'$.decision') END
  WHERE s.sub = ? AND d.sub = ? AND s.conv IS NULL AND d.conv IS NULL
   AND s.sender = d.recipient AND s.ref_id = d.ref_id AND s.ref_fp = d.ref_fp`, envelope.SubStatus, envelope.SubDecision)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var host, hostKey, id, key, body, decisionID, decisionBody string
		if err := rows.Scan(&host, &hostKey, &id, &key, &body, &decisionID, &decisionBody); err != nil {
			return out, err
		}
		var status envelope.Status
		var decision envelope.Decision
		if json.Unmarshal([]byte(body), &status) != nil || json.Unmarshal([]byte(decisionBody), &decision) != nil {
			continue
		}
		r, ok := reports[decision.Report]
		if !ok || r.Host != host || r.Key != hostKey || status.Decision != decisionID || status.Report != r.ID || status.Attempt != decision.Attempt || status.Refused != "" || status.At <= 0 {
			continue
		}
		switch status.State {
		case "queued", "running", "resolved", "answered", "declined", "cancelled":
		default:
			continue
		}
		switch decision.Action {
		case "accept", "resolve", "reply", "decline", "cancel":
		case "continue":
			if !nativeDecision(decision) {
				continue
			}
		default:
			continue
		}
		for _, it := range r.Report.Items {
			native := nativeEligibility[reportTransition(r, it)]
			if native {
				response, e := parseNativeResponse(decision.Text)
				if e != nil || response.Token != it.Native.Token {
					continue
				}
			}
			if (it.Actionable || native) && it.ID == id && it.Key == key && it.Attempt == decision.Attempt && it.State == decision.Expect {
				out[reportTransition(r, it)] = true
			}
		}
	}
	return out, rows.Err()
}

func remoteReviewCopy(counts map[string]int, local int) string {
	var parts []string
	if local > 0 {
		parts = append(parts, fmt.Sprintf("%d local request(s) need your decision", local))
	}
	hosts := make([]string, 0, len(counts))
	for host := range counts {
		hosts = append(hosts, host)
	}
	slices.Sort(hosts)
	for _, host := range hosts {
		parts = append(parts, fmt.Sprintf("%d remote request(s) on %s need your decision", counts[host], host))
	}
	return strings.Join(parts, "; ") + ". "
}
