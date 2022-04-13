package main

import (
	"sync"

	"github.com/LukeEmmet/html2gemini"
	"github.com/mkhoi1998/Stack-on-Go/stackongo"
	"github.com/nleeper/goment"
	"github.com/patrickmn/go-cache"
	"github.com/pitr/gig"

	// 	"github.com/nkall/compactnumber"

	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	g := gig.Default()
	cacheStore := cache.New(15*time.Minute, 20*time.Minute)
	// 	formatter := compactnumber.NewFormatter("en-US", compactnumber.Short)

	// Routes
	g.Handle("/", func(c gig.Context) error {
		content := `# GemStack!
StackExchange mirrors for a smaller web.

***Search Operators Cheat Sheet
Search Operators.

[tag] search within a tag           user:1234 search by author
"words here" exact phrase           collective:"Name" collective content
answers:0 unanswered questions      score:3 posts with a 3+ score
is:question type of post            isaccepted:yes search within status
***
=> /search 🔎 Search

## Recent questions:`
		content = strings.ReplaceAll(content, "***", "```")

		session := stackongo.NewSession("stackoverflow")
		params := make(stackongo.Params)
		params.Sort("hot")
		params.Pagesize(5)

		questions, found := cacheStore.Get("homepage")
		if !found {
			var err error
			questions, err = session.AllQuestions(params)
			if err != nil {
				return err
			}

			cacheStore.Set("homepage", questions, 45*time.Minute)
		}

		var err error
		content, err = renderQuestionList(questions.(*stackongo.Questions), content)
		if err != nil {
			return err
		}

		return c.Gemini(content)
	})

	g.Handle("/question", func(c gig.Context) error {
		query, err := c.QueryString()
		if err != nil {
			return err
		}

		id, err := strconv.Atoi(query)
		if err != nil {
			return err
		}

		session := stackongo.NewSession("stackoverflow")
		params := make(stackongo.Params)
		params.Add("filter", "!-MBrU_IzpJ5H-AG6Bbzy.X-BYQe(2v-.J")
		params.Sort("votes")

		questions, err := session.GetQuestions([]int{id}, params)
		if err != nil {
			return err
		}

		for _, question := range questions.Items {
			creation_date, err := goment.New(time.Unix(question.Creation_date, 0))
			if err != nil {
				return err
			}

			last_activity_date, err := goment.New(time.Unix(question.Last_activity_date, 0))
			if err != nil {
				return err
			}

			ctx := html2gemini.NewTraverseContext(html2gemini.Options{})
			questionBody, err := html2gemini.FromString(html.UnescapeString(question.Body), *ctx)
			if err != nil {
				return err
			}

			// ToDo: replace limited HTML parsing with native Markdown converted into GemText
			content := fmt.Sprintf("# [%d] %s\nAsked %s · Modified %s · Viewed %d times\n\n\n%s\n\n## %d Answers:",
				question.Score, html.UnescapeString(question.Title), creation_date.FromNow(), last_activity_date.FromNow(), question.View_count, questionBody, question.Answer_count)

			sort.Slice(question.Answers, func(i, j int) bool {
				return question.Answers[i].Score > question.Answers[j].Score
			})

			var renderedAnswers = make([]string, len(question.Answers))

			var wg sync.WaitGroup
			wg.Add(len(question.Answers))

			for i, answer := range question.Answers {
				errorChannel := make(chan error)
				answer := answer
				i := i
				go func() {
					ctx := html2gemini.NewTraverseContext(html2gemini.Options{})
					answerBody, err := html2gemini.FromString(html.UnescapeString(answer.Body), *ctx)
					if err != nil {
						errorChannel <- err
					}

					creation_date, err := goment.New(time.Unix(answer.Creation_date, 0))
					if err != nil {
						errorChannel <- err
					}

					renderedAnswers[i] = fmt.Sprintf("\n### [%d] Answer by %s\nAnswered %s\n\n%s\n\n",
						answer.Score, answer.Owner.Display_name, strings.ToLower(creation_date.Calendar()), answerBody)
					wg.Done()
				}()
			}

			wg.Wait()
			content += strings.Join(renderedAnswers, "")
			return c.Gemini(content)
		}
		return c.NoContent(gig.StatusBadRequest, "Unknown error")
	})

	g.Handle("/search", func(c gig.Context) error {
		query, err := c.QueryString()
		if err != nil {
			return err
		}

		if query == "" {
			return c.NoContent(gig.StatusInput, "Search query")
		}

		session := stackongo.NewSession("stackoverflow")
		params := make(stackongo.Params)
		params.Sort("votes")
		// params.Add("tagged", "")

		var content = fmt.Sprintf("# Results for «%s»:\n=> /search 🔎 Search", query)
		// Switch to advanced search
		results, err := session.AdvancedSearch([]string{query}, params)

		content, err = renderQuestionList(results, content)
		if err != nil {
			return err
		}

		return c.Gemini(content)
	})

	err := g.Run("my.crt", "my.key")
	if err != nil {
		panic(err)
	}
}

func renderQuestionList(questions *stackongo.Questions, content string) (string, error) {
	var renderedQuestions = make([]string, len(questions.Items))

	var wg sync.WaitGroup
	wg.Add(len(questions.Items))

	for i, question := range questions.Items {
		errorChannel := make(chan error)
		question := question
		i := i
		go func() {
			creation_date, err := goment.New(time.Unix(question.Creation_date, 0))
			if err != nil {
				errorChannel <- err
			}

			last_activity_date, err := goment.New(time.Unix(question.Last_activity_date, 0))
			if err != nil {
				errorChannel <- err
			}

			tag_string := ""
			for _, tag := range question.Tags {
				tag_string += fmt.Sprintf("[%s] ", tag)
			}

			view_count := question.View_count

			renderedQuestions[i] = fmt.Sprintf("\n\n=>/question?%d [%d] · %s\nAnswered %d times · Asked %s · Modified %s · Viewed %d times\n%s",
				question.Question_id, question.Score, html.UnescapeString(question.Title), question.Answer_count, creation_date.FromNow(), last_activity_date.FromNow(), view_count, tag_string)
			errorChannel <- nil
			wg.Done()
		}()
		err := <-errorChannel
		if err != nil {
			return "", err
		}
	}
	wg.Wait()
	content += strings.Join(renderedQuestions, "")
	return content, nil
}
