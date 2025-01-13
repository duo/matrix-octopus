package octopusid

import (
	"fmt"
	"strings"

	"maunium.net/go/mautrix/bridgev2/networkid"
)

func MakeUserID(id string) networkid.UserID {
	return networkid.UserID(id)
}

func MakeUserLoginID(user string) networkid.UserLoginID {
	return networkid.UserLoginID(MakeUserID(user))
}

func MakeMessageID(chat string, id string) networkid.MessageID {
	return networkid.MessageID(fmt.Sprintf("%s:%s", chat, id))
}

func MakeFakeMessageID(chat string, data string) networkid.MessageID {
	return networkid.MessageID(fmt.Sprintf("fake:%s:%s", chat, data))
}

type ParsedMessageID struct {
	Chat string
	ID   string
}

func ParseMessageID(messageID networkid.MessageID) (*ParsedMessageID, error) {
	parts := strings.SplitN(string(messageID), ":", 2)
	if len(parts) == 2 {
		if parts[0] == "fake" {
			return nil, fmt.Errorf("fake message ID")
		}
		return &ParsedMessageID{Chat: parts[0], ID: parts[1]}, nil
	} else {
		return nil, fmt.Errorf("invalid message ID")
	}
}
