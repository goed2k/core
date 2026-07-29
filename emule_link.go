package goed2k

import (
	"bufio"
	"encoding/base64"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/goed2k/core/protocol"
)

type LinkType string

const (
	LinkServer  LinkType = "SERVER"
	LinkServers LinkType = "SERVERS"
	LinkNodes       LinkType = "NODES"
	LinkFile        LinkType = "FILE"
	LinkCollection  LinkType = "COLLECTION"
)

type EMuleLink struct {
	Hash         protocol.Hash
	AICHRootHash protocol.AICHHash
	PartHashes   []protocol.Hash
	NumberValue  int64
	StringValue  string
	Type         LinkType
	FileLinks    []EMuleLink
}

func ParseEMuleLink(uri string) (EMuleLink, error) {
	if uri == "" {
		return EMuleLink{}, NewError(LinkMailformed)
	}

	decURI, err := url.QueryUnescape(uri)
	if err != nil {
		return EMuleLink{}, NewError(UnsupportedEncoding)
	}

	parts := strings.Split(decURI, "|")
	if len(parts) < 2 || parts[0] != "ed2k://" || parts[len(parts)-1] != "/" {
		return EMuleLink{}, NewError(LinkMailformed)
	}

	switch parts[1] {
	case "server":
		if len(parts) != 5 {
			return EMuleLink{}, NewError(LinkMailformed)
		}
		port, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return EMuleLink{}, NewError(NumberFormatError)
		}
		return EMuleLink{NumberValue: port, StringValue: parts[2], Type: LinkServer}, nil
	case "serverlist":
		if len(parts) != 4 {
			return EMuleLink{}, NewError(LinkMailformed)
		}
		return EMuleLink{StringValue: parts[2], Type: LinkServers}, nil
	case "nodeslist":
		if len(parts) != 4 {
			return EMuleLink{}, NewError(LinkMailformed)
		}
		return EMuleLink{StringValue: parts[2], Type: LinkNodes}, nil
	case "ed2kcollection", "collection":
		if len(parts) != 5 {
			return EMuleLink{}, NewError(LinkMailformed)
		}
		name, err := url.QueryUnescape(parts[2])
		if err != nil {
			return EMuleLink{}, NewError(UnsupportedEncoding)
		}
		payload, err := url.QueryUnescape(parts[3])
		if err != nil {
			return EMuleLink{}, NewError(UnsupportedEncoding)
		}
		fileLinks, err := ParseEMuleCollectionContent(payload)
		if err != nil {
			return EMuleLink{}, err
		}
		return EMuleLink{
			StringValue: name,
			Type:        LinkCollection,
			FileLinks:   fileLinks,
		}, nil
	case "file":
		if len(parts) < 6 {
			return EMuleLink{}, NewError(LinkMailformed)
		}
		hash, err := protocol.HashFromString(parts[4])
		if err != nil {
			return EMuleLink{}, NewError(InternalError)
		}
		size, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return EMuleLink{}, NewError(NumberFormatError)
		}
		name, err := url.QueryUnescape(parts[2])
		if err != nil {
			return EMuleLink{}, NewError(UnsupportedEncoding)
		}
		link := EMuleLink{
			Hash:        hash,
			NumberValue: size,
			StringValue: name,
			Type:        LinkFile,
		}
		for i := 5; i < len(parts)-1; i++ {
			segment := parts[i]
			if segment == "" {
				continue
			}
			if strings.HasPrefix(segment, "h=") {
				root, err := protocol.AICHHashFromString(strings.TrimPrefix(segment, "h="))
				if err != nil {
					return EMuleLink{}, NewError(LinkMailformed)
				}
				link.AICHRootHash = root
				continue
			}
			if strings.HasPrefix(segment, "p=") {
				raw := strings.TrimPrefix(segment, "p=")
				for _, pieceHex := range strings.Split(raw, ":") {
					if pieceHex == "" {
						continue
					}
					pieceHash, err := protocol.HashFromString(pieceHex)
					if err != nil {
						return EMuleLink{}, NewError(LinkMailformed)
					}
					link.PartHashes = append(link.PartHashes, pieceHash)
				}
			}
		}
		return link, nil
	}

	return EMuleLink{}, NewError(UnknownLinkType)
}

func ParseEMuleCollectionContent(content string) ([]EMuleLink, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, NewError(LinkMailformed)
	}
	if strings.HasPrefix(strings.ToLower(content), "ed2k://") {
		return parseEMuleCollectionLines(content)
	}
	if decoded, err := base64.StdEncoding.DecodeString(content); err == nil {
		if links, err := parseEMuleCollectionLines(string(decoded)); err == nil && len(links) > 0 {
			return links, nil
		}
	}
	return parseEMuleCollectionLines(content)
}

func ParseEMuleCollectionFile(path string) ([]EMuleLink, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, NewError(LinkMailformed)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseEMuleCollectionLines(string(data))
}

func parseEMuleCollectionLines(content string) ([]EMuleLink, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	links := make([]EMuleLink, 0, 4)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		link, err := ParseEMuleLink(line)
		if err != nil {
			continue
		}
		if link.Type != LinkFile {
			continue
		}
		links = append(links, link)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, NewError(LinkMailformed)
	}
	return links, nil
}
