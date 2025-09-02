package mocks

import (
	"os/exec"
	"strings"
)

// CommandExecutor interface for mocking command execution
type CommandExecutor interface {
	Command(name string, args ...string) *exec.Cmd
	Output(cmd *exec.Cmd) ([]byte, error)
	Run(cmd *exec.Cmd) error
}

// MockCommandExecutor mocks command execution
type MockCommandExecutor struct {
	Commands []MockCommand
	CallLog  []string
}

type MockCommand struct {
	Command string
	Output  string
	Error   error
}

func NewMockCommandExecutor() *MockCommandExecutor {
	return &MockCommandExecutor{
		Commands: make([]MockCommand, 0),
		CallLog:  make([]string, 0),
	}
}

func (m *MockCommandExecutor) AddCommand(command, output string, err error) {
	m.Commands = append(m.Commands, MockCommand{
		Command: command,
		Output:  output,
		Error:   err,
	})
}

func (m *MockCommandExecutor) Command(name string, args ...string) *exec.Cmd {
	cmdStr := name + " " + strings.Join(args, " ")
	m.CallLog = append(m.CallLog, cmdStr)

	// Return a dummy command for now - we'll handle execution in Output/Run
	return &exec.Cmd{}
}

func (m *MockCommandExecutor) Output(cmd *exec.Cmd) ([]byte, error) {
	if len(m.Commands) == 0 {
		return []byte(""), nil
	}

	// Return the first matching command
	mock := m.Commands[0]
	if len(m.Commands) > 1 {
		m.Commands = m.Commands[1:]
	}

	return []byte(mock.Output), mock.Error
}

func (m *MockCommandExecutor) Run(cmd *exec.Cmd) error {
	if len(m.Commands) == 0 {
		return nil
	}

	// Return the first matching command
	mock := m.Commands[0]
	if len(m.Commands) > 1 {
		m.Commands = m.Commands[1:]
	}

	return mock.Error
}

func (m *MockCommandExecutor) GetCallLog() []string {
	return m.CallLog
}

func (m *MockCommandExecutor) ClearCallLog() {
	m.CallLog = make([]string, 0)
}
