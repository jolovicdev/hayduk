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
}

func NewHello(version string, team bool) Hello {
	return Hello{Type: KindHello, Proto: ProtoVersion, Version: version, Team: team}
}

// Snapshot carries the full campaign state, sent on every (re)connect.
type Snapshot struct {
	Type  string        `json:"type"`
	State CampaignState `json:"state"`
}

func NewSnapshot(s CampaignState) Snapshot { return Snapshot{Type: KindSnapshot, State: s} }

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
	Error      string `json:"error,omitempty"`
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
	ID          string    `json:"id"`
	Type        string    `json:"type"` // meterpreter|shell
	TunnelPeer  string    `json:"tunnelPeer,omitempty"`
	ViaExploit  string    `json:"viaExploit,omitempty"`
	ViaPayload  string    `json:"viaPayload,omitempty"`
	Info        string    `json:"info,omitempty"`
	Username    string    `json:"username,omitempty"`
	TargetHost  string    `json:"targetHost,omitempty"`
	SessionHost string    `json:"sessionHost,omitempty"`
	UUID        string    `json:"uuid,omitempty"`
	OpenedAt    time.Time `json:"openedAt,omitzero"`
}

type JobState struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	StartedAt time.Time `json:"startedAt,omitzero"`
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
}

// ResourceUpdate refreshes one resource. Exactly the field matching Resource
// is set; the rest stay nil and are omitted on the wire.
type ResourceUpdate struct {
	Type        string                   `json:"type"`
	Resource    string                   `json:"resource"`
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

func ConsoleUpdate(c *ConsoleState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResConsole, Console: c}
}

func InteractUpdate(i *InteractState) ResourceUpdate {
	return ResourceUpdate{Type: KindResource, Resource: ResInteract, Interact: i}
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

// SessionOutputMsg streams output of the attached session.
type SessionOutputMsg struct {
	Type string `json:"type"`
	SID  string `json:"sid"`
	Data string `json:"data"`
}

// ConsoleOutputMsg streams console output.
type ConsoleOutputMsg struct {
	Type string `json:"type"`
	Data string `json:"data"`
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
	MethodConnect        = "connect"
	MethodDisconnect     = "disconnect"
	MethodConsoleWrite   = "console.write"
	MethodConsoleTabs    = "console.tabs"
	MethodModuleInfo     = "module.info"
	MethodModuleOptions  = "module.options"
	MethodPayloads       = "module.compatible_payloads"
	MethodModuleExecute  = "module.execute"
	MethodSessionAttach  = "session.attach"
	MethodSessionDetach  = "session.detach"
	MethodSessionWrite   = "session.write"
	MethodSessionStop    = "session.stop"
	MethodSessionUpgrade = "session.upgrade"
	MethodWorkspaceList  = "workspace.list"
	MethodWorkspaceSet   = "workspace.set"
	MethodDBRefresh      = "db.refresh"
	MethodAttacksFind    = "attacks.find"
	MethodReportHTML     = "report.html"
	MethodHailMary       = "campaign.hail_mary"
)

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

type AttacksFindParams struct {
	Host string `json:"host"`
}

type ReportPayload struct {
	HTML string `json:"html"`
}

type HailMaryParams struct {
	Hosts      []string `json:"hosts"`
	MaxPerHost int      `json:"maxPerHost"`
}

type HailMaryPayload struct {
	Planned int `json:"planned"`
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
	Name string `json:"name"`
	Reason string `json:"reason"`
	// Port is the service port the match was struck on; 0 when the matcher
	// had no port to attribute. Hail Mary forwards it as RPORT so modules
	// hit the port the service was actually found on.
	Port int `json:"port"`
}

type AttacksPayload struct {
	Host    string        `json:"host"`
	Matches []AttackMatch `json:"matches"`
}

// ServerMessageFromEvent converts an EventEntry to its wire message.
func ServerMessageFromEvent(e *EventEntry) EventMsg {
	return EventMsg{Type: KindEvent, Seq: e.Seq, Time: e.Time, Level: e.Level, Text: e.Text, Operator: e.Operator}
}
