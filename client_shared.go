package goed2k

import (
	"github.com/monkeyWie/goed2k/protocol"
)

// SharedFiles 返回共享库快照。
func (c *Client) SharedFiles() []*SharedFile {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.SharedFiles()
}

// AddSharedDir 见 Session.AddSharedDir。
func (c *Client) AddSharedDir(path string) error {
	if err := c.session.AddSharedDir(path); err != nil {
		return err
	}
	_ = c.saveStateIfConfigured()
	c.emitStatusUpdate()
	return nil
}

// RemoveSharedDir 见 Session.RemoveSharedDir。
func (c *Client) RemoveSharedDir(path string) error {
	if err := c.session.RemoveSharedDir(path); err != nil {
		return err
	}
	_ = c.saveStateIfConfigured()
	c.emitStatusUpdate()
	return nil
}

// ListSharedDirs 见 Session.ListSharedDirs。
func (c *Client) ListSharedDirs() []string {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.ListSharedDirs()
}

// RescanSharedDirs 见 Session.RescanSharedDirs。
func (c *Client) RescanSharedDirs() error {
	if err := c.session.RescanSharedDirs(); err != nil {
		return err
	}
	_ = c.saveStateIfConfigured()
	c.emitStatusUpdate()
	return nil
}

// ImportSharedFile 见 Session.ImportSharedFile。
func (c *Client) ImportSharedFile(path string) error {
	if err := c.session.ImportSharedFile(path); err != nil {
		return err
	}
	_ = c.saveStateIfConfigured()
	c.emitStatusUpdate()
	return nil
}

// RemoveSharedFile 从共享库移除。
func (c *Client) RemoveSharedFile(hash protocol.Hash) bool {
	if !c.session.RemoveSharedFile(hash) {
		return false
	}
	_ = c.saveStateIfConfigured()
	c.emitStatusUpdate()
	return true
}
