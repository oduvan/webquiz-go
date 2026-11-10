package quiz

import "math/rand/v2"

// RandomOrder returns the question ids in a random order, like
// generate_random_question_order. A question with stick_to_the_previous stays
// right after the question before it: such questions form groups, and the
// groups are shuffled, not the questions. rnd may be nil to use the global
// random source.
func RandomOrder(questions []Question, rnd *rand.Rand) []int {
	var groups [][]int
	var current []int
	for _, question := range questions {
		if question.Sticky() && len(current) > 0 {
			current = append(current, question.ID())
			continue
		}
		if len(current) > 0 {
			groups = append(groups, current)
		}
		current = []int{question.ID()}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}

	swap := func(i, j int) { groups[i], groups[j] = groups[j], groups[i] }
	if rnd != nil {
		rnd.Shuffle(len(groups), swap)
	} else {
		rand.Shuffle(len(groups), swap)
	}

	order := make([]int, 0, len(questions))
	for _, group := range groups {
		order = append(order, group...)
	}
	return order
}
