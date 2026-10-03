package dynamic

// promoteChoice moves the tier's chosen candidate to the front for the modes
// whose choice is not a sort order: random and round-robin selection, and a
// pace ranking whose leading candidates are indistinguishable because their
// usage is unknown. Everything else keeps the sorted order.
func promoteChoice(ranked []RankCandidate, tier Tier, options RankOptions) {
	eligible := eligiblePositions(ranked)
	if len(eligible) < 2 {
		return
	}
	switch tier.Policy.Mode {
	case TierModeRandom:
		moveToFront(ranked, eligible[options.pick(len(eligible))])
	case TierModeRoundRobin:
		moveToFront(ranked, nextAfterLast(ranked, eligible, options.LastPicked[tier.HeadID]))
	case TierModePace:
		ties := unknownLeadingTies(ranked, eligible)
		if len(ties) > 1 {
			moveToFront(ranked, ties[options.pick(len(ties))])
		}
	}
}

func eligiblePositions(ranked []RankCandidate) []int {
	positions := make([]int, 0, len(ranked))
	for index, entry := range ranked {
		if entry.Eligible {
			positions = append(positions, index)
		}
	}
	return positions
}

// nextAfterLast returns the first eligible position after the last chosen
// candidate in saved row order, wrapping around. Without a previous choice, or
// when it is no longer in the tier, the first eligible candidate is chosen.
// Ineligible candidates are skipped, so a blocked candidate never stalls the
// rotation.
func nextAfterLast(ranked []RankCandidate, eligible []int, lastID string) int {
	lastIndex := -1
	for index, entry := range ranked {
		if entry.Candidate.ID == lastID {
			lastIndex = index
			break
		}
	}
	if lastIndex < 0 {
		return eligible[0]
	}
	for _, position := range eligible {
		if position > lastIndex {
			return position
		}
	}
	return eligible[0]
}

// unknownLeadingTies returns the eligible candidates that share the leading
// pace position while their provider usage is unknown and their recorded usage
// is equal. Choosing among them at random keeps a tier whose usage nobody
// knows from sending every new session to its first row.
func unknownLeadingTies(ranked []RankCandidate, eligible []int) []int {
	first := ranked[eligible[0]]
	if paceGroup(first.Score) != paceGroupUnknown {
		return nil
	}
	ties := []int{eligible[0]}
	for _, position := range eligible[1:] {
		entry := ranked[position]
		if paceGroup(entry.Score) != paceGroupUnknown ||
			internalLess(first.Score.Internal, entry.Score.Internal) ||
			internalLess(entry.Score.Internal, first.Score.Internal) {
			break
		}
		ties = append(ties, position)
	}
	return ties
}

func moveToFront(ranked []RankCandidate, position int) {
	if position <= 0 || position >= len(ranked) {
		return
	}
	chosen := ranked[position]
	copy(ranked[1:position+1], ranked[0:position])
	ranked[0] = chosen
}
