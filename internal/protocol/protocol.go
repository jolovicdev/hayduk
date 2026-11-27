// Package protocol defines the WebSocket wire contract between hayduk and
// the browser. Field names and JSON tags are frozen; the TypeScript types in
// ui/src/protocol/types.ts are generated from this file and must not drift.
package protocol

import (
	"encoding/json"
	"time"
)

// Wire message kinds.
const (
	KindHello         = "hello"
	KindSnapshot      = "snapshot"
	KindResource      = "resource"
	KindEvent         = "event"
	KindSessionOutput = "sessionOutput"
	KindConsoleOutput = "consoleOutput"
	KindResponse      = "response"
	KindCommand       = "command"
)

// Resource names carried by resource updates.
const (
	ResConnection  = "connection"
	ResHosts       = "hosts"
	ResServices    = "services"
	ResSessions    = "sessions"
	ResJobs        = "jobs"
	ResCreds       = "creds"
	ResLoot        = "loot"
	ResModules     = "modules"
	ResConsole     = "console"
	ResInteract    = "interact"
	ResRoutes      = "routes"
	ResModuleRanks = "moduleRanks"
	ResOperators   = "operators"
	ResOperations  = "operations"
	ResListeners   = "listeners"
)

// Error codes.
const (
	CodeUnknownMethod   = "unknown_method"
	CodeBadParams       = "bad_params"
	CodeNotConnected    = "not_connected"
	CodeBusy            = "busy"
	CodeConnectFailed   = "connect_failed"
	CodeSessionNotFound = "session_not_found"
	CodeInvalidOption   = "invalid_option"
	CodeTimeout         = "command_timeout"
	CodeRPC             = "rpc_error"
	CodeUnexpected      = "unexpected_response"
	CodeInternal        = "internal"
)

// Event levels.
const (
	LevelInfo    = "info"
	LevelSuccess = "success"
	LevelWarn    = "warn"
	LevelError   = "error"
	// LevelChat marks a team chat message: Text is the message itself and
	// Operator its author. Chat rides the shared event log, Armitage-style.
	LevelChat = "chat"
)

const ProtoVersion = 1

// ServerMessage is any server→client message value. The bus and server move
// them as opaque payloads; marshaling happens at the socket.
type ServerMessage = any

// Hello is the first message after the socket opens.
type Hello struct {
	Type    string `json:"type"`
	Proto   int    `json:"proto"`
	Version string `json:"version"`
	Team    bool   `json:"team"`
	// Identity is the operator name bound to this connection's access token
	// (per-operator tokens in team mode). Empty for the shared token, where
	// the browser picks its own label.
	Identity string `json:"identity,omitempty"`
}

func NewHello(version string, team bool) Hello {
	return Hello{Type: KindHello, Proto: ProtoVersion, Version: version, Team: team}
}

// Snapshot carries the full campaign state, sent on every (re)connect.
// Operator scopes the personal resources (console, interact) to one team
// member; empty is the local single-operator view. Each socket receives
// only its own snapshot.
type Snapshot struct {
	Type     string        `json:"type"`
	Operator string        `json:"operator,omitempty"`
	State    CampaignState `json:"state"`
}

func NewSnapshot(s CampaignState) Snapshot { return NewSnapshotFor("", s) }

func NewSnapshotFor(operator string, s CampaignState) Snapshot {
	return Snapshot{Type: KindSnapshot, Operator: operator, State: s}
}

// ConnectionState is one of the resources in CampaignState and also rides
// the connect command response.
type ConnectionState struct {
	Status     string `json:"status"` // disconnected|connecting|connected|reconnecting
	Host       string `json:"host"`
	Port       int    `json:"port"`
	SSL        bool   `json:"ssl"`
	Username   string `json:"username"`
	MSFVersion string `json:"msfVersion"`
	Workspace  string `json:"workspace"`
	// FileStaging is "local" when this process shares a filesystem with
	// msfrpcd and browser file transfers can be staged through it, "remote"
	// otherwise. Remote links keep pure-RPC tools; downloads then report the
	// path on the msfrpcd host instead of returning bytes.
	FileStaging string `json:"fileStaging,omitempty"`
	Error       string `json:"error,omitempty"`
}

type HostState struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Mac       string `json:"mac"`
	OSName    string `json:"osName"`
	OSFlavor  string `json:"osFlavor"`
	OSVersion string `json:"osVersion"`
	Purpose   string `json:"purpose"`
	Info      string `json:"info"`
	Comments  string `json:"comments"`
}

type ServiceState struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Proto string `json:"proto"`
	Name  string `json:"name"`
	State string `json:"state"`
	Info  string `json:"info"`
}

type SessionState struct {
	ID          string `json:"id"`
	Type        string `json:"type"` // meterpreter|shell
	TunnelPeer  string `json:"tunnelPeer,omitempty"`
	ViaExploit  string `json:"viaExploit,omitempty"`
	ViaPayload  string `json:"viaPayload,omitempty"`
	Info        string `json:"info,omitempty"`
	Username    string `json:"username,omitempty"`
	TargetHost  string `json:"targetHost,omitempty"`
	SessionHost string `json:"sessionHost,omitempty"`
	UUID        string `json:"uuid,omitempty"`
	// Workspace is the msf workspace active when the session opened; empty
	// for sessions picked up without one. Reports scope sessions on it.
	Workspace string    `json:"workspace,omitempty"`
	OpenedAt  time.Time `json:"openedAt,omitzero"`
	// OpenedBy names the operator whose launch opened the session (team
	// mode attribution); empty when unknown.
	OpenedBy string `json:"openedBy,omitempty"`
}

type JobState struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	StartedAt time.Time `json:"startedAt,omitzero"`
}

// Operation kinds and statuses. An operation is one engine-owned long-running
// task (a service scan, a brute-force spray, a Hail Mary) that outlives the
// browser command that started it and is visible to every operator.
const (
	OpScan     = "scan"
	OpBrute    = "brute"
	OpSpray    = "spray"
	OpHailMary = "hailmary"

	OpRunning  = "running"
	OpDone     = "done"
	OpFailed   = "failed"
	OpCanceled = "canceled"
)

// ListenerState is one registered multi/handler launch. Job ids are the
// daemon's, so the registry is reconciled against the live job list on every
// connect.
type ListenerState struct {
	JobID     string    `json:"jobId"`
	Payload   string    `json:"payload,omitempty"`
	Lhost     string    `json:"lhost,omitempty"`
	Lport     int       `json:"lport,omitempty"`
	Operator  string    `json:"operator,omitempty"`
	StartedAt time.Time `json:"startedAt,omitzero"`
}

// OperationState is the wire view of one operation. Total is 0 when the size
// is not known up front (a service scan); Done may sit below Total when the
// run ends early.
type OperationState struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	Operator   string    `json:"operator,omitempty"`
	Workspace  string    `json:"workspace,omitempty"`
	Target     string    `json:"target,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	Done       int       `json:"done"`
	Total      int       `json:"total"`
	StartedAt  time.Time `json:"startedAt,omitzero"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}

// RouteState is one pivot route: a subnet reachable through a session.
type RouteState struct {
	Subnet    string `json:"subnet"`
	SessionID string `json:"sessionId"`
}

type CredState struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Proto   string `json:"proto"`
	Service string `json:"service"`
	User    string `json:"user"`
	Pass    string `json:"pass"`
	Type    string `json:"type"`
}

// LootState omits the loot data payload on purpose: it can be huge and the
// table only needs identity and description.
type LootState struct {
	Host string `json:"host"`
	Type string `json:"type"`
	Name string `json:"name"`
	Info string `json:"info"`
}

type ModuleIndex struct {
	Exploits  []string `json:"exploits"`
	Auxiliary []string `json:"auxiliary"`
	Post      []string `json:"post"`
	Payloads  []string `json:"payloads"`
	Encoders  []string `json:"encoders"`
	Nops      []string `json:"nops"`
	Evasion   []string `json:"evasion"`
}

type ConsoleState struct {
	Output string `json:"output"`
}

type InteractState struct {
	SID    string `json:"sid"`
	Output string `json:"output"`
}

type EventEntry struct {
	Seq      int64     `json:"seq"`
	Time     time.Time `json:"time"`
	Level    string    `json:"level"`
	Text     string    `json:"text"`
	Operator string    `json:"operator,omitempty"`
	// Workspace is the msf workspace active when the event was recorded;
	// empty for framework-wide events. Reports filter events on it.
	Workspace string `json:"workspace,omitempty"`
}

type CampaignState struct {
	Connection  ConnectionState          `json:"connection"`
	Hosts       []*HostState             `json:"hosts"`
	Services    []*ServiceState          `json:"services"`
	Sessions    map[string]*SessionState `json:"sessions"`
	Jobs        map[string]*JobState     `json:"jobs"`
	Routes      []*RouteState            `json:"routes"`
	Creds       []*CredState             `json:"creds"`
	Loot        []*LootState             `json:"loot"`
	Modules     *ModuleIndex             `json:"modules"`
	ModuleRanks map[string]string        `json:"moduleRanks"`
	Console     *ConsoleState            `json:"console"`
	Interact    *InteractState           `json:"interact"`
	Events      []*EventEntry            `json:"events"`
	Operators   []string                 `json:"operators"`
	Operations  []*OperationState        `json:"operations"`
	Listeners   []*ListenerState         `json:"listeners"`
}

// ResourceUpdate refreshes one resource. Exactly the field matching Resource
// is set; the rest stay nil and are omitted on the wire. Operator scopes the
// personal resources (console, interact) to one team member; empty is the
// local single-operator view.
type ResourceUpdate struct {
	Type        string                   `json:"type"`
	Resource    string                   `json:"resource"`
	Operator    string                   `json:"operator,omitempty"`
	Connection  *ConnectionState         `json:"connection,omitempty"`
	Hosts       []*HostState             `json:"hosts,omitempty"`
	Services    []*ServiceState          `json:"services,omitempty"`
	Sessions    map[string]*SessionState `json:"sessions,omitempty"`
	Jobs        map[string]*JobState     `json:"jobs,omitempty"`
	Routes      []*RouteState            `json:"routes,omitempty"`
	Creds       []*CredState             `json:"creds,omitempty"`
	Loot        []*LootState             `json:"loot,omitempty"`
	Modules     *ModuleIndex             `json:"modules,omitempty"`
	ModuleRanks map[string]string        `json:"moduleRanks,omitempty"`
	Console     *ConsoleState            `json:"console,omitempty"`
	Interact    *InteractState           `json:"interact,omitempty"`
	Operators   []string                 `json:"operators,omitempty"`
	Operations  []*OperationState        `json:"operations,omitempty"`
	Listeners   []*ListenerState         `json:"listeners,omitempty"`
}

func ConnectionUpdate(c ConnectionState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResConnection, Connection: &c}
}

func HostsUpdate(h []*HostState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResHosts, Hosts: h}
}

func ServicesUpdate(s []*ServiceState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResServices, Services: s}
}

func SessionsUpdate(s map[string]*SessionState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResSessions, Sessions: s}
}

func JobsUpdate(j map[string]*JobState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResJobs, Jobs: j}
}

func RoutesUpdate(r []*RouteState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResRoutes, Routes: r}
}

func CredsUpdate(c []*CredState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResCreds, Creds: c}
}

func LootUpdate(l []*LootState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResLoot, Loot: l}
}

func ModulesUpdate(m *ModuleIndex) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResModules, Modules: m}
}

// ModuleRanksUpdate carries a batch of freshly prefetched module ranks; the
// client merges it over the cached map.
func ModuleRanksUpdate(ranks map[string]string) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResModuleRanks, ModuleRanks: ranks}
}

// OperatorsUpdate carries the connected-operator list (team mode).
func OperatorsUpdate(names []string) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResOperators, Operators: names}
}

// OperationsUpdate carries every known operation; the list is small and the
// client replaces its copy wholesale.
func OperationsUpdate(ops []*OperationState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResOperations, Operations: ops}
}

// ListenersUpdate carries the registered handler launches.
func ListenersUpdate(listeners []*ListenerState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResListeners, Listeners: listeners}
}

func ConsoleUpdate(operator string, c *ConsoleState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResConsole, Operator: operator, Console: c}
}

func InteractUpdate(operator string, i *InteractState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResInteract, Operator: operator, Interact: i}
}

// EventMsg appends one entry to the operator-visible event log.
type EventMsg struct {
	Type     string    `json:"type"`
	Seq      int64     `json:"seq"`
	Time     time.Time `json:"time"`
	Level    string    `json:"level"`
	Text     string    `json:"text"`
	Operator string    `json:"operator,omitempty"`
}

// SessionOutputMsg streams output of the attached session. Session
// transcripts are shared team-wide: every socket receives them and keeps
// the sessions it has open.
type SessionOutputMsg struct {
	Type string `json:"type"`
	SID  string `json:"sid"`
	Data string `json:"data"`
}

// ConsoleOutputMsg streams console output. Operator scopes the chunk to one
// team member's console; empty is the local single-operator console.
type ConsoleOutputMsg struct {
	Type     string `json:"type"`
	Operator string `json:"operator,omitempty"`
	Data     string `json:"data"`
}

// CommandResponse answers a client command; Data is set when OK.
type CommandResponse struct {
	Type  string          `json:"type"`
	ID    int64           `json:"id"`
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *ErrorBody      `json:"error,omitempty"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func OKResponse(id int64, data any) CommandResponse {
	raw, err := json.Marshal(data)
	if err != nil {
		return ErrorResponse(id, CodeInternal, err.Error())
	}
	return CommandResponse{Type: KindResponse, ID: id, OK: true, Data: raw}
}

func ErrorResponse(id int64, code, message string) CommandResponse {
	return CommandResponse{Type: KindResponse, ID: id, OK: false, Error: &ErrorBody{Code: code, Message: message}}
}

// ClientMessage is the only client→server message. Operator names the
// acting operator in team mode.
type ClientMessage struct {
	Type     string          `json:"type"`
	ID       int64           `json:"id"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params,omitempty"`
	Operator string          `json:"operator,omitempty"`
}

// Command methods.
const (
	MethodConnect         = "connect"
	MethodDisconnect      = "disconnect"
	MethodConsoleWrite    = "console.write"
	MethodConsoleTabs     = "console.tabs"
	MethodModuleInfo      = "module.info"
	MethodModuleOptions   = "module.options"
	MethodPayloads        = "module.compatible_payloads"
	MethodModuleExecute   = "module.execute"
	MethodModuleCheck     = "module.check"
	MethodSessionAttach   = "session.attach"
	MethodSessionDetach   = "session.detach"
	MethodSessionWrite    = "session.write"
	MethodSessionStop     = "session.stop"
	MethodSessionUpgrade  = "session.upgrade"
	MethodWorkspaceList   = "workspace.list"
	MethodWorkspaceSet    = "workspace.set"
	MethodWorkspaceAdd    = "workspace.add"
	MethodWorkspaceDelete = "workspace.delete"
	MethodOperatorJoin    = "operator.join"
	MethodTeamChat        = "team.chat"
	MethodDBRefresh       = "db.refresh"
	MethodAttacksFind     = "attacks.find"
	MethodReportHTML      = "report.html"
	MethodHailMary        = "campaign.hail_mary"
	MethodJobStop         = "job.stop"
	MethodSessionRun      = "session.run"
	MethodPayloadGenerate = "payload.generate"
	// MethodSessionFsList lists a remote directory through the session's
	// meterpreter channel and returns structured rows.
	MethodSessionFsList = "session.fs_list"
	// MethodSessionFileDownload returns the file's bytes when msfrpcd shares
	// this filesystem, and the msfrpcd-side path otherwise.
	MethodSessionFileDownload = "session.file_download"
	// MethodSessionFileUpload stages browser bytes on the shared filesystem
	// and pushes them through the session. Requires local file staging.
	MethodSessionFileUpload = "session.file_upload"
	// MethodSessionScreenshot captures the remote screen; bytes ride back
	// only with local file staging.
	MethodSessionScreenshot = "session.screenshot"
	// MethodSessionPost runs a post module through the session channel so
	// its output streams into the interact transcript.
	MethodSessionPost = "session.post"
	// MethodSessionPostModules lists post modules compatible with a session.
	MethodSessionPostModules = "session.post_modules"
	MethodHostAdd            = "db.host_add"
	MethodHostDelete         = "db.host_delete"
	MethodServiceDelete      = "db.service_delete"
	MethodDBImport           = "db.import"
	MethodHostNote           = "db.host_note"
	MethodCredAdd            = "db.cred_add"
	MethodCredDelete         = "db.cred_delete"
	MethodLootDownload       = "loot.download"
	// MethodServicesScan runs a version-detecting nmap scan through the
	// Metasploit console and imports the results into the workspace.
	MethodServicesScan = "services.scan"
	// MethodBruteStart runs a login spray as an engine operation, so it
	// survives the browser tab that started it and is visible to the team.
	MethodBruteStart = "brute.start"
	// MethodOperationsCancel cancels a running engine operation.
	MethodOperationsCancel = "operations.cancel"
	// MethodProfileList returns saved msfrpcd connection profiles (they
	// carry no password). It works while disconnected.
	MethodProfileList = "profile.list"
)

// ProfilePayload is one saved connection. Passwords are never stored.
type ProfilePayload struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	SSL      bool   `json:"ssl"`
	Username string `json:"username"`
}

type ConnectParams struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	SSL      bool   `json:"ssl"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ConsoleWriteParams struct {
	Command string `json:"command"`
}

type ConsoleTabsParams struct {
	Line string `json:"line"`
}

type ModuleRefParams struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type ModuleExecuteParams struct {
	Type           string                 `json:"type"`
	Name           string                 `json:"name"`
	Options        map[string]interface{} `json:"options"`
	Payload        string                 `json:"payload,omitempty"`
	PayloadOptions map[string]interface{} `json:"payloadOptions,omitempty"`
}

type ModuleCheckParams struct {
	Type    string                 `json:"type"`
	Name    string                 `json:"name"`
	Options map[string]interface{} `json:"options"`
}

type ModuleCheckPayload struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Output  string `json:"output"`
}

type SessionRefParams struct {
	SID string `json:"sid"`
}

type SessionWriteParams struct {
	SID  string `json:"sid"`
	Data string `json:"data"`
}

type SessionUpgradeParams struct {
	SID   string `json:"sid"`
	LHOST string `json:"lhost"`
	LPORT int    `json:"lport"`
}

type WorkspaceSetParams struct {
	Name string `json:"name"`
}

// TeamChatParams carries one team chat message, broadcast to every operator
// through the shared event log.
type TeamChatParams struct {
	Text string `json:"text"`
}

type AttacksFindParams struct {
	Host string `json:"host"`
}

// ServiceScanParams describes a version-detecting service scan. Ports empty
// means nmap's top 1000 TCP ports; SkipDiscovery adds -Pn for hosts that do
// not answer ping.
type ServiceScanParams struct {
	Target        string `json:"target"`
	Ports         string `json:"ports,omitempty"`
	SkipDiscovery bool   `json:"skipDiscovery,omitempty"`
}

type ServiceScanPayload struct {
	Target      string `json:"target"`
	Ports       string `json:"ports,omitempty"`
	OperationID string `json:"operationId,omitempty"`
}

// BruteParams describes a login spray. Users and Passwords are raw lines from
// the dialog; the engine builds and caps the plan so the run is authoritative
// on the server. Ports carries a scanned port per host; Port overrides every
// host when set. Userless scanners (VNC) ignore Users.
type BruteParams struct {
	Hosts         []string       `json:"hosts"`
	Module        string         `json:"module"`
	Users         []string       `json:"users,omitempty"`
	Passwords     []string       `json:"passwords,omitempty"`
	Ports         map[string]int `json:"ports,omitempty"`
	Port          int            `json:"port,omitempty"`
	AddBlank      bool           `json:"addBlank,omitempty"`
	AddUserAsPass bool           `json:"addUserAsPass,omitempty"`
	Userless      bool           `json:"userless,omitempty"`
}

type BrutePayload struct {
	OperationID string `json:"operationId"`
	Planned     int    `json:"planned"`
	Capped      int    `json:"capped"`
}

type OperationRefParams struct {
	ID string `json:"id"`
}

type ReportPayload struct {
	HTML string `json:"html"`
}

type HailMaryParams struct {
	Hosts      []string `json:"hosts"`
	MaxPerHost int      `json:"maxPerHost"`
}

type HailMaryPayload struct {
	Planned     int    `json:"planned"`
	OperationID string `json:"operationId,omitempty"`
}

type JobRefParams struct {
	ID string `json:"id"`
}

type SessionRunParams struct {
	SID     string `json:"sid"`
	Command string `json:"command"`
}

type SessionRunPayload struct {
	Output string `json:"output"`
}

type SessionFsListParams struct {
	SID  string `json:"sid"`
	Path string `json:"path,omitempty"`
}

// FsEntry is one row of a remote directory listing. Kind is "dir" or "file".
type FsEntry struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Kind     string `json:"kind"`
	Modified string `json:"modified,omitempty"`
}

type FsListPayload struct {
	Path      string    `json:"path"`
	Separator string    `json:"separator"`
	Entries   []FsEntry `json:"entries"`
}

type SessionFileRefParams struct {
	SID  string `json:"sid"`
	Path string `json:"path"`
}

// FileTransferPayload carries a captured file back to the browser. DataBase64
// is set only when the bytes could be staged locally; otherwise SavedPath
// names where the file landed on the msfrpcd host.
type FileTransferPayload struct {
	Filename   string `json:"filename"`
	DataBase64 string `json:"dataBase64,omitempty"`
	Size       int    `json:"size,omitempty"`
	SavedPath  string `json:"savedPath,omitempty"`
	Output     string `json:"output,omitempty"`
}

type SessionFileUploadParams struct {
	SID        string `json:"sid"`
	Path       string `json:"path"`
	DataBase64 string `json:"dataBase64"`
}

type SessionPostParams struct {
	SID     string                 `json:"sid"`
	Module  string                 `json:"module"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type SessionPostPayload struct {
	Command string `json:"command"`
}

type SessionPostModulesPayload struct {
	Modules []string `json:"modules"`
}

type SessionDetachParams struct {
	SID string `json:"sid,omitempty"`
}

type PayloadGenerateParams struct {
	Payload string                 `json:"payload"`
	Options map[string]interface{} `json:"options"`
	Format  string                 `json:"format"`
}

type PayloadGeneratePayload struct {
	Filename  string `json:"filename"`
	Content   string `json:"content,omitempty"`
	SavedPath string `json:"savedPath,omitempty"`
}

type HostAddParams struct {
	Address  string `json:"address"`
	Name     string `json:"name,omitempty"`
	OSName   string `json:"osName,omitempty"`
	Comments string `json:"comments,omitempty"`
}

type HostRefParams struct {
	Address string `json:"address"`
}

type ServiceRefParams struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Proto string `json:"proto"`
}

type DBImportParams struct {
	Data string `json:"data"`
}

type HostNoteParams struct {
	Address  string `json:"address"`
	Comments string `json:"comments"`
}

type CredAddParams struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Service string `json:"service"`
	User    string `json:"user"`
	Pass    string `json:"pass"`
}

type CredRefParams struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Service string `json:"service"`
	User    string `json:"user"`
}

type LootRefParams struct {
	Host string `json:"host"`
	Type string `json:"type"`
	Name string `json:"name"`
}

type LootPayload struct {
	Data string `json:"data"`
}

// Response payloads.

type ModuleInfoPayload struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Rank        string             `json:"rank"`
	Authors     []string           `json:"authors"`
	References  []ModuleRefPayload `json:"references"`
	Targets     []string           `json:"targets"`
	FilePath    string             `json:"filePath"`
}

type ModuleRefPayload struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type ModuleOptionPayload struct {
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Advanced bool     `json:"advanced"`
	Desc     string   `json:"desc"`
	Default  any      `json:"default"`
	Enums    []string `json:"enums"`
}

type ExecPayload struct {
	JobID int    `json:"jobId"`
	UUID  string `json:"uuid"`
}

type AttackMatch struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
	// Port is the service port the match was struck on; 0 when the matcher
	// had no port to attribute. Hail Mary forwards it as RPORT so modules
	// hit the port the service was actually found on.
	Port int `json:"port"`
	// Rank is the cached Metasploit reliability rank when known.
	Rank string `json:"rank,omitempty"`
}

type AttacksPayload struct {
	Host    string        `json:"host"`
	Matches []AttackMatch `json:"matches"`
}

// ServerMessageFromEvent converts an EventEntry to its wire message.
func ServerMessageFromEvent(e *EventEntry) EventMsg {
	return EventMsg{Type: KindEvent, Seq: e.Seq, Time: e.Time, Level: e.Level, Text: e.Text, Operator: e.Operator}
}
