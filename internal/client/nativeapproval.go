package client

// A native request belongs to one owned Codex process. Its presentation is
// private owner data; neither the presentation nor a received name is authority.
import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

const nativeResponseDomain = "agentnet-native-response-v1\n"
const nativeOperationMax = 12 << 10

type NativeQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}
type NativeQuestion struct {
	ID       string                 `json:"id"`
	Header   string                 `json:"header"`
	Question string                 `json:"question"`
	IsOther  bool                   `json:"isOther,omitempty"`
	IsSecret bool                   `json:"isSecret,omitempty"`
	Options  []NativeQuestionOption `json:"options,omitempty"`
}
type NativeFormField struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}
type NativeRequest struct {
	Token       string            `json:"token"`
	Harness     string            `json:"harness"`
	Host        string            `json:"host"`
	Request     string            `json:"request"`
	Key         string            `json:"key"`
	Attempt     int64             `json:"attempt"`
	Method      string            `json:"method"`
	Thread      string            `json:"thread"`
	Turn        string            `json:"turn"`
	Item        string            `json:"item,omitempty"`
	ApprovalID  string            `json:"approval_id,omitempty"`
	AgentID     string            `json:"agent_id,omitempty"`
	Operation   string            `json:"operation,omitempty"`
	CWD         string            `json:"cwd,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Choices     []string          `json:"choices"`
	CancelRun   bool              `json:"cancel_run,omitempty"`
	Questions   []NativeQuestion  `json:"questions,omitempty"`
	Fields      []NativeFormField `json:"fields,omitempty"`
	Unsupported string            `json:"unsupported,omitempty"`
	Expires     int64             `json:"expires"` // existing worker deadline, milliseconds
}
type nativeCallback struct {
	ID     json.RawMessage
	Params json.RawMessage
	View   NativeRequest
}
type nativeResponse struct {
	Token   string                     `json:"token"`
	Choice  string                     `json:"choice"`
	Answers map[string]string          `json:"answers,omitempty"`
	Values  map[string]json.RawMessage `json:"values,omitempty"`
}

func parseNativeResponse(text string) (nativeResponse, error) {
	var r nativeResponse
	if !strings.HasPrefix(text, nativeResponseDomain) || !utf8.ValidString(text) || len(text) > 16<<10 {
		return r, errors.New("choose the exact current native request")
	}
	err := decodeStrict([]byte(strings.TrimPrefix(text, nativeResponseDomain)), &r)
	return r, err
}

func codexNativeCallback(method string, id, raw, item json.RawMessage, cwd, thread, turn string) (nativeCallback, error) {
	c := nativeCallback{ID: append(json.RawMessage(nil), id...), Params: append(json.RawMessage(nil), raw...), View: NativeRequest{Harness: "codex", Method: method, Thread: thread, Turn: turn, CWD: cwd}}
	var p struct {
		ThreadID              string            `json:"threadId"`
		TurnID                string            `json:"turnId"`
		ItemID                string            `json:"itemId"`
		ApprovalID            string            `json:"approvalId"`
		Command               string            `json:"command"`
		CWD                   string            `json:"cwd"`
		Reason                string            `json:"reason"`
		Message               string            `json:"message"`
		Mode                  string            `json:"mode"`
		Kind                  string            `json:"kind"`
		EnvironmentID         string            `json:"environmentId"`
		GrantRoot             string            `json:"grantRoot"`
		ServerName            string            `json:"serverName"`
		Permissions           json.RawMessage   `json:"permissions"`
		AdditionalPermissions json.RawMessage   `json:"additionalPermissions"`
		Network               json.RawMessage   `json:"networkApprovalContext"`
		Available             []json.RawMessage `json:"availableDecisions"`
		Questions             []NativeQuestion  `json:"questions"`
		Schema                json.RawMessage   `json:"requestedSchema"`
	}
	if len(id) == 0 || len(id) > 256 || len(raw) > 64<<10 || json.Unmarshal(raw, &p) != nil || p.ThreadID != thread || thread == "" || turn == "" || (p.TurnID != turn && !(method == "mcpServer/elicitation/request" && p.TurnID == "")) {
		return c, errors.New("native callback does not belong to this owned run")
	}
	c.View.Item, c.View.ApprovalID = p.ItemID, p.ApprovalID
	if p.CWD != "" {
		c.View.CWD = p.CWD
	}
	c.View.Reason = p.Reason
	unsupported := func(why string) {
		c.View.Unsupported = why
		c.View.Choices = []string{"deny", "cancel"}
		c.View.Questions = nil
		c.View.Fields = nil
	}
	switch method {
	case "item/commandExecution/requestApproval":
		c.View.Operation = p.Command
		if c.View.Operation == "" {
			var it struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Command string `json:"command"`
				CWD     string `json:"cwd"`
			}
			if json.Unmarshal(item, &it) == nil && it.ID == p.ItemID && it.Type == "commandExecution" {
				c.View.Operation = it.Command
				if p.CWD == "" && it.CWD != "" {
					c.View.CWD = it.CWD
				}
			}
		}
		commandComplete := strings.TrimSpace(c.View.Operation) != ""
		var network struct {
			Host     string `json:"host"`
			Protocol string `json:"protocol"`
		}
		networkComplete := json.Unmarshal(p.Network, &network) == nil && strings.TrimSpace(network.Host) != "" && slices.Contains([]string{"http", "https", "socks5Tcp", "socks5Udp"}, network.Protocol)
		for _, extra := range []struct {
			label string
			raw   json.RawMessage
		}{{"Additional native permissions", p.AdditionalPermissions}, {"Native network request", p.Network}} {
			if len(extra.raw) > 0 && string(extra.raw) != "null" {
				c.View.Operation += "\n" + extra.label + ": " + string(extra.raw)
			}
		}
		c.View.Choices = []string{"allow", "deny", "cancel"}
		if p.Available != nil {
			c.View.Choices = nil
			for _, entry := range p.Available {
				var decision string
				if json.Unmarshal(entry, &decision) == nil {
					switch decision {
					case "accept":
						c.View.Choices = append(c.View.Choices, "allow")
					case "decline":
						c.View.Choices = append(c.View.Choices, "deny")
					case "cancel":
						c.View.Choices = append(c.View.Choices, "cancel")
					}
				}
			}
		}
		if !commandComplete && !networkComplete || c.View.CWD == "" {
			unsupported("Codex did not provide the exact command and working folder. This action cannot be allowed here.")
		}
		if p.EnvironmentID != "" {
			c.View.Operation += "\nNative execution environment: " + p.EnvironmentID
		}
		if p.Kind != "" && p.Kind != "command" {
			unsupported("This native approval concerns terminal input or an unsupported action kind (" + p.Kind + "). It cannot be approved from a command preview. Deny or cancel it here.")
		}
	case "item/fileChange/requestApproval":
		var it struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Status  string `json:"status"`
			Changes []struct {
				Path string          `json:"path"`
				Kind json.RawMessage `json:"kind"`
				Diff *string         `json:"diff"`
			} `json:"changes"`
		}
		if decodeStrict(item, &it) != nil || it.ID != p.ItemID || it.Type != "fileChange" || len(it.Changes) == 0 {
			unsupported("Codex did not provide the matching file operation. This action cannot be allowed here.")
		} else {
			for _, change := range it.Changes {
				var kind struct {
					Type     string          `json:"type"`
					MovePath json.RawMessage `json:"move_path,omitempty"`
				}
				if change.Path == "" || change.Diff == nil || decodeStrict(change.Kind, &kind) != nil || !slices.Contains([]string{"add", "delete", "update"}, kind.Type) || kind.Type != "update" && len(kind.MovePath) > 0 {
					unsupported("The matching native file operation is incomplete.")
					break
				}
				if len(kind.MovePath) > 0 && string(kind.MovePath) != "null" {
					var path string
					if json.Unmarshal(kind.MovePath, &path) != nil {
						unsupported("The native move destination is invalid.")
						break
					}
				}
				c.View.Operation += fmt.Sprintf("%s %s\n%s\n", change.Path, change.Kind, *change.Diff)
			}
			if c.View.Unsupported == "" {
				c.View.Choices = []string{"allow", "deny", "cancel"}
			}
		}
		if p.GrantRoot != "" {
			c.View.Operation += "\nProposed session write root: " + p.GrantRoot
			unsupported("This native request also proposes write access for the remainder of the session. AgentNet cannot grant a session permission here. Deny or cancel this request.")
		}
	case "item/permissions/requestApproval":
		c.View.CancelRun = true
		if len(p.Permissions) == 0 || string(p.Permissions) == "null" || p.CWD == "" {
			unsupported("Codex did not provide the exact native permissions and folder.")
		} else {
			c.View.Operation = "Native permissions for this turn only:\n" + string(p.Permissions)
			c.View.Choices = []string{"allow", "deny", "cancel"}
		}
	case "item/tool/requestUserInput", "tool/requestUserInput":
		c.View.CancelRun = true
		c.View.Choices = []string{"submit", "cancel"}
		c.View.Questions = p.Questions
		seen := map[string]bool{}
		if len(p.Questions) == 0 || len(p.Questions) > 12 {
			unsupported("This native input is not a supported bounded question form.")
			c.View.Choices = []string{"cancel"}
		}
		for _, q := range p.Questions {
			if q.ID == "" || seen[q.ID] || q.Question == "" || len(q.Options) > 32 {
				unsupported("This native question form is incomplete or ambiguous.")
				c.View.Choices = []string{"cancel"}
				break
			}
			seen[q.ID] = true
		}
	case "mcpServer/elicitation/request":
		c.View.Operation = p.ServerName + ": " + p.Message
		c.View.Choices = []string{"deny", "cancel"}
		if p.ServerName == "" {
			return c, errors.New("native MCP server identity is missing")
		}
		if p.Mode == "form" {
			fields, err := nativeFormFields(p.Schema)
			if err != nil {
				unsupported("This native MCP form uses an unsupported schema. Decline or cancel it here; AgentNet cannot supply its authenticated proof.")
			} else {
				c.View.Fields = fields
				c.View.Choices = []string{"submit", "deny", "cancel"}
			}
		} else {
			unsupported("This native MCP request requires an authenticated or unsupported native interaction (" + p.Mode + "). AgentNet cannot approve it here. Decline or cancel this request.")
		}
	default:
		return c, errors.New("unsupported native callback")
	}
	if c.View.Item == "" && method != "mcpServer/elicitation/request" {
		return c, errors.New("native item identity is missing")
	}
	presentation, _ := json.Marshal(c.View)
	if len(presentation) > nativeOperationMax {
		c.View.Operation = ""
		c.View.Reason = ""
		unsupported("The native operation is too large to display in full here. It cannot be allowed from a truncated preview.")
		if strings.Contains(method, "requestUserInput") {
			c.View.Choices = []string{"cancel"}
		}
	}
	// Even an unsupported preview may return only decisions actually offered by
	// the native request. If none remain, Cancel stops our owned worker instead.
	if method == "item/commandExecution/requestApproval" && p.Available != nil {
		offered := map[string]bool{}
		for _, entry := range p.Available {
			var decision string
			if json.Unmarshal(entry, &decision) == nil {
				switch decision {
				case "accept":
					offered["allow"] = true
				case "decline":
					offered["deny"] = true
				case "cancel":
					offered["cancel"] = true
				}
			}
		}
		c.View.Choices = slices.DeleteFunc(c.View.Choices, func(choice string) bool { return !offered[choice] })
	}
	if len(c.View.Choices) == 0 {
		c.View.Choices = []string{"cancel"}
		c.View.CancelRun = true
		c.View.Unsupported = "The native request offers no supported one-action decision. Cancel this run here."
	}
	return c, nil
}

// Standard MCP forms deliberately support flat string/boolean fields only,
// with bounded string enums. Other schema semantics are never silently ignored.
func nativeFormFields(raw json.RawMessage) ([]NativeFormField, error) {
	var s struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if decodeStrict(raw, &s) != nil || s.Type != "object" || len(s.Properties) == 0 || len(s.Properties) > 12 {
		return nil, errors.New("unsupported MCP form")
	}
	ids := make([]string, 0, len(s.Properties))
	for id := range s.Properties {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range s.Required {
		if _, ok := s.Properties[id]; !ok {
			return nil, errors.New("unknown required native field")
		}
	}
	var fields []NativeFormField
	for _, id := range ids {
		var f NativeFormField
		if decodeStrict(s.Properties[id], &struct {
			Type        *string   `json:"type"`
			Title       *string   `json:"title,omitempty"`
			Description *string   `json:"description,omitempty"`
			Enum        *[]string `json:"enum,omitempty"`
		}{&f.Type, &f.Title, &f.Description, &f.Enum}) != nil || (f.Type != "string" && f.Type != "boolean") || len(f.Enum) > 32 || f.Type == "boolean" && len(f.Enum) > 0 {
			return nil, errors.New("unsupported native field")
		}
		f.ID = id
		f.Required = slices.Contains(s.Required, id)
		fields = append(fields, f)
	}
	return fields, nil
}

func nativeCallbackResult(c nativeCallback, r nativeResponse) (json.RawMessage, error) {
	if !slices.Contains(c.View.Choices, r.Choice) || len(r.Answers) > 0 && r.Choice != "submit" || len(r.Values) > 0 && r.Choice != "submit" {
		return nil, errors.New("choose an offered native response")
	}
	if r.Choice == "cancel" && c.View.CancelRun {
		return nil, nil
	}
	var result any
	switch c.View.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		result = map[string]string{"decision": map[string]string{"allow": "accept", "deny": "decline", "cancel": "cancel"}[r.Choice]}
	case "item/permissions/requestApproval":
		var p struct {
			Permissions json.RawMessage `json:"permissions"`
		}
		_ = json.Unmarshal(c.Params, &p)
		permission := json.RawMessage(`{}`)
		if r.Choice == "allow" {
			permission = p.Permissions
		}
		result = map[string]any{"permissions": permission, "scope": "turn"}
	case "item/tool/requestUserInput", "tool/requestUserInput":
		if r.Choice == "cancel" {
			return nil, nil
		} // cancel the owned worker; no fabricated answer
		if len(r.Answers) != len(c.View.Questions) || len(r.Values) != 0 {
			return nil, errors.New("answer every exact native question")
		}
		answers := map[string]any{}
		for _, q := range c.View.Questions {
			text, ok := r.Answers[q.ID]
			if !ok || strings.TrimSpace(text) == "" {
				return nil, errors.New("answer every exact native question")
			}
			if len(q.Options) > 0 && !q.IsOther && !slices.ContainsFunc(q.Options, func(o NativeQuestionOption) bool { return o.Label == text }) {
				return nil, errors.New("choose an offered native option")
			}
			answers[q.ID] = map[string]any{"answers": []string{text}}
		}
		result = map[string]any{"answers": answers}
	case "mcpServer/elicitation/request":
		if len(r.Answers) != 0 {
			return nil, errors.New("use the exact native form")
		}
		res := map[string]any{"action": map[string]string{"submit": "accept", "deny": "decline", "cancel": "cancel"}[r.Choice]}
		if r.Choice == "submit" {
			content := map[string]any{}
			for id, raw := range r.Values {
				f, ok := slices.BinarySearchFunc(c.View.Fields, id, func(f NativeFormField, id string) int { return strings.Compare(f.ID, id) })
				if !ok {
					return nil, errors.New("unknown native form field")
				}
				field := c.View.Fields[f]
				if field.Type == "boolean" {
					var v bool
					if json.Unmarshal(raw, &v) != nil || string(raw) == "null" {
						return nil, errors.New("native form field requires a boolean")
					}
					content[id] = v
				} else {
					var v string
					if json.Unmarshal(raw, &v) != nil || string(raw) == "null" || len(field.Enum) > 0 && !slices.Contains(field.Enum, v) {
						return nil, errors.New("native form field requires an offered string")
					}
					content[id] = v
				}
			}
			for _, f := range c.View.Fields {
				if f.Required {
					if _, ok := content[f.ID]; !ok {
						return nil, errors.New("complete required native form fields")
					}
				}
			}
			res["content"] = content
		}
		result = res
	default:
		return nil, errors.New("unsupported native method")
	}
	return json.Marshal(result)
}

func nativePermissionGrant(method string, result json.RawMessage) bool {
	if method == "item/commandExecution/requestApproval" || method == "item/fileChange/requestApproval" {
		var r struct {
			Decision string `json:"decision"`
		}
		_ = json.Unmarshal(result, &r)
		return r.Decision == "accept"
	}
	if method == "item/permissions/requestApproval" {
		var r struct {
			Permissions map[string]json.RawMessage `json:"permissions"`
		}
		_ = json.Unmarshal(result, &r)
		return len(r.Permissions) > 0
	}
	return false
}
