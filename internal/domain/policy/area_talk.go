package policy

// TalkArea holds the talk keys of the policy: the chat room, team memory and the human and silence deadlines.
type TalkArea struct {
	Memory   Memory   `yaml:"memory" json:"memory"`
	Timeouts Timeouts `yaml:"timeouts" json:"timeouts"`
	Chat     Chat     `yaml:"chat" json:"chat"`
}

// Memory says which rooms and chat keywords feed team memory (TECH_SPEC 9).
type Memory struct {
	Rooms        []string `yaml:"rooms" json:"rooms"`
	ChatKeywords []string `yaml:"chat_keywords" json:"chat_keywords"`
}

// Timeouts are the human and silence deadlines (TECH_SPEC 16). Silence never moves a change toward merge.
type Timeouts struct {
	Question      Duration `yaml:"question" json:"question"`
	RemindAfter   Duration `yaml:"remind_after" json:"remind_after"`
	EscalateAfter Duration `yaml:"escalate_after" json:"escalate_after"`
	RejectAfter   Duration `yaml:"reject_after" json:"reject_after"`
}

// Chat is where the change room lives and how much the bot says.
type Chat struct {
	Bridge          string `yaml:"bridge" json:"bridge"`
	Verbosity       string `yaml:"verbosity" json:"verbosity"`
	DecisionsInRoom bool   `yaml:"decisions_in_room" json:"decisions_in_room"`
}
