package main

import (
	"encoding/json"
	"strings"
	"sync"
	"unicode"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sagemakerruntime"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
)

type Plugin struct {
	plugin.MattermostPlugin

	configuration *configuration

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
}
type val struct {
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

func Predict(message string) string {
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String("us-east-2"),
		Credentials: credentials.NewStaticCredentials("AKIA4H5E5ZK6O5METHPI", "VLf3lB67/YuL+xZs8tJjJbhckATGLN3rsBrk5SGj", ""),
	})
	if err != nil {
		panic(err)
	}

	svc := sagemakerruntime.New(sess)

	body := `{"inputs": "%s"}`

	body = strings.Replace(body, "%s", message, 1)

	params := sagemakerruntime.InvokeEndpointInput{}
	params.SetAccept("application/json")
	params.SetContentType("application/json")
	params.SetBody([]byte(body))
	params.SetEndpointName("huggingface-tensorflow-inference-2021-10-20-03-19-53-382")

	req, out := svc.InvokeEndpointRequest(&params)
	if err := req.Send(); err != nil {
		// process error
		panic(err)
	}

	var in []val
	if err := json.Unmarshal(out.Body, &in); err != nil {
		panic(err)
	}

	return in[0].Label
}

func (p *Plugin) FilterPost(post *model.Post) (*model.Post, string) {
	configuration := p.getConfiguration()
	_, fromBot := post.GetProps()["from_bot"]

	if configuration.ExcludeBots && fromBot {
		return post, ""
	}

	postMessageWithoutAccents := removeAccents(post.Message)

	toxic := Predict(postMessageWithoutAccents)

	sb := "LABEL_0"

	// If message in not toxic, do not block the messag
	if toxic == sb {
		return post, ""
	}

	p.API.SendEphemeralPost(post.UserId, &model.Post{
		ChannelId: post.ChannelId,
		Message:   "Message not allowed because it is inappropriate",
		RootId:    post.RootId,
	})

	return nil, "Message not allowed because it is inappropriate"
}

func (p *Plugin) MessageWillBePosted(_ *plugin.Context, post *model.Post) (*model.Post, string) {
	return p.FilterPost(post)
}

func (p *Plugin) MessageWillBeUpdated(_ *plugin.Context, newPost *model.Post, _ *model.Post) (*model.Post, string) {
	return p.FilterPost(newPost)
}

func removeAccents(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	output, _, e := transform.String(t, s)
	if e != nil {
		return s
	}

	return output
}
