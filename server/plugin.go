package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sagemakerruntime"

	"github.com/aaaton/golem/v4"
	"github.com/aaaton/golem/v4/dicts/en"

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

func (p *Plugin) Predict(message string) string {
	configuration := p.getConfiguration()
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(configuration.Region),
		Credentials: credentials.NewStaticCredentials(configuration.AccessKeyID, configuration.SecretAccessKeyID, ""),
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
	params.SetEndpointName(configuration.EndpointName)

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

	lemmatizer, err := golem.New(en.New())
	if err != nil {
		panic(err)
	}

	reg := regexp.MustCompile("[^a-zA-Z]+")

	postMessageWithoutAccents = reg.ReplaceAllString(postMessageWithoutAccents, " ")

	words := strings.Fields(postMessageWithoutAccents)

	for i, x := range words {
		words[i] = lemmatizer.Lemma(x) // note the = instead of :=
	}

	postMessageWithoutAccents = strings.Join(words, " ")
	toxic := p.Predict(postMessageWithoutAccents)

	sb := "LABEL_0"

	// If message in not toxic, do not block the messag
	if toxic == sb {
		return post, ""
	}

	p.API.SendEphemeralPost(post.UserId, &model.Post{
		ChannelId: post.ChannelId,
		Message:   configuration.WarningMessage,
		RootId:    post.RootId,
	})

	return nil, configuration.WarningMessage
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
