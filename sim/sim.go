package sim

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

type Sex string

const (
	Male   Sex = "M"
	Female Sex = "F"
)

type Agent struct {
	ID           int            `json:"id"`
	X, Y         int            `json:"x"`
	Energy       float64        `json:"energy"`
	Sex          Sex            `json:"sex"`
	Age          int            `json:"age"`
	Aggression   float64        `json:"agg"`
	Speed        int            `json:"spd"`
	Strength     float64        `json:"strength"`
	Repro        float64        `json:"repro"`
	Experience   map[string]int `json:"exp"`
	Weights      [][]float64    `json:"-"`
	LearningRate float64        `json:"-"`
	LastState    []float64      `json:"-"`
	LastAction   int            `json:"-"`
	LastProbs    []float64      `json:"-"`
	PolicyDir    int            `json:"policy_dir"`
	Parents      []int          `json:"parents"`
	Hunger       int            `json:"-"`

	CriticW     []float64 `json:"-"`
	Gamma       float64   `json:"-"`
	CriticLR    float64   `json:"-"`
	EntropyBeta float64   `json:"-"`
	AdvClip     float64   `json:"-"`

	RMean     float64 `json:"-"`
	RVar      float64 `json:"-"`
	REstAlpha float64 `json:"-"`
	REps      float64 `json:"-"`
}

type Food struct {
	X, Y   int     `json:"x"`
	Energy float64 `json:"energy"`
}

type Event struct {
	Type     string `json:"type"`
	Tick     int    `json:"tick"`
	ActorID  int    `json:"actor_id"`
	ActorSex Sex    `json:"actor_sex"`
	TargetID int    `json:"target_id,omitempty"`
	Message  string `json:"message"`
}

type Sim struct {
	W, H int

	mu     sync.Mutex
	agents map[int]*Agent
	foods  map[int]*Food
	nextID int

	StateChan chan interface{}

	rand            *rand.Rand
	totalDeaths     int
	totalAgeAtDeath int
	totalBirths     int
	lineage         map[int][]int
	RandomFood      bool
	RandomFoodProb  float64
	ticksElapsed    int
	events          []Event
}

func NewSim(w, h int) *Sim {
	s := &Sim{
		W: w, H: h,
		agents:         make(map[int]*Agent),
		foods:          make(map[int]*Food),
		StateChan:      make(chan interface{}, 10),
		rand:           rand.New(rand.NewSource(time.Now().UnixNano())),
		lineage:        make(map[int][]int),
		RandomFood:     true,
		RandomFoodProb: 0.04,
		events:         make([]Event, 0),
	}
	for i := 0; i < 2; i++ {
		s.addRandomAgent()
	}
	return s
}

func (s *Sim) addRandomAgent() {
	s.nextID++
	a := &Agent{
		ID:         s.nextID,
		X:          s.rand.Intn(s.W),
		Y:          s.rand.Intn(s.H),
		Energy:     100 + s.rand.Float64()*50,
		Sex:        []Sex{Male, Female}[s.rand.Intn(2)],
		Age:        0,
		Aggression: s.rand.Float64(),
		Speed:      1 + s.rand.Intn(2),
		Strength:   5 + s.rand.Float64()*10,
		Repro:      s.rand.Float64()*0.35 + 0.3,
		Experience: map[string]int{},
	}
	na := 9
	nf := 5
	a.Weights = make([][]float64, na)
	for i := 0; i < na; i++ {
		a.Weights[i] = make([]float64, nf)
		for j := 0; j < nf; j++ {
			a.Weights[i][j] = s.rand.NormFloat64() * 0.1
		}
	}
	a.LearningRate = 0.03
	a.LastState = make([]float64, nf)
	a.LastProbs = make([]float64, na)
	a.CriticW = make([]float64, nf)
	a.Gamma = 0.98
	a.CriticLR = 2 * a.LearningRate
	a.EntropyBeta = 0.01
	a.AdvClip = 6.0
	a.REstAlpha = 0.01
	a.REps = 1e-8
	a.PolicyDir = 4
	a.Hunger = 0
	s.agents[a.ID] = a
	s.totalBirths++
	s.lineage[a.ID] = []int{}
}

func (s *Sim) addEvent(eventType string, actorID int, actorSex Sex, targetID int, message string) {
	if len(s.events) > 5000 {
		s.events = s.events[1:]
	}
	s.events = append(s.events, Event{
		Type:     eventType,
		Tick:     s.ticksElapsed,
		ActorID:  actorID,
		ActorSex: actorSex,
		TargetID: targetID,
		Message:  message,
	})
}

func (s *Sim) Run() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		s.Tick()
	}
}

func (s *Sim) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ticksElapsed++

	attempts := (s.W * s.H) / 50
	for i := 0; i < attempts; i++ {
		if s.RandomFood && s.rand.Float64() < s.RandomFoodProb {
			x := s.rand.Intn(s.W)
			y := s.rand.Intn(s.H)
			key := x*s.H + y
			if !s.foodAt(x, y) && !s.agentAt(x, y) {
				s.foods[key] = &Food{X: x, Y: y, Energy: 12 + s.rand.Float64()*12}
			}
		}
	}

	for id, a := range s.agents {
		a.Age++
		a.Energy -= 0.08
		a.Hunger++
		if a.Hunger > 100 {
			a.Energy -= 0.15
		}
		if a.Energy <= 0 {
			s.totalDeaths++
			s.totalAgeAtDeath += a.Age
			s.addEvent("death", a.ID, a.Sex, 0, fmt.Sprintf("Агент %d (%s) умер от голода в возрасте %d", a.ID, a.Sex, a.Age))
			delete(s.agents, id)
			continue
		}

		oldEnergy := a.Energy
		oldKills := a.Experience["kills"]
		oldRepro := a.Experience["repro"]

		features, probs, act := s.chooseAction(a)
		a.LastState = features
		a.LastProbs = probs
		a.LastAction = act
		a.PolicyDir = act

		oldDist := s.distanceToNearestFood(a)
		dx := (act % 3) - 1
		dy := (act / 3) - 1
		nx := clamp(a.X+dx, 0, s.W-1)
		ny := clamp(a.Y+dy, 0, s.H-1)
		a.X = nx
		a.Y = ny
		newDist := s.distanceToNearestFood(a)

		distReward := oldDist - newDist

		if fkey, ok := s.foodAtKey(a.X, a.Y); ok {
			f := s.foods[fkey]
			a.Energy += f.Energy
			delete(s.foods, fkey)
			a.Experience["ate"]++
			a.Hunger = 0
			distReward += 2.0
		}

		_ = s.tryAttack(a)
		_ = s.tryMerge(a)
		_ = s.tryReproduce(a)

		if _, exists := s.agents[a.ID]; !exists {
			continue
		}

		reward := (a.Energy - oldEnergy)
		reward += float64(a.Experience["kills"]-oldKills) * 5.0
		reward += float64(a.Experience["repro"]-oldRepro) * 3.0
		reward += distReward * 1.5
		s.updateActorCritic(a, features, probs, act, reward)
	}

	agentsList := make([]*Agent, 0, len(s.agents))
	for _, a := range s.agents {
		agentsList = append(agentsList, a)
	}
	agentsOut := make([]map[string]interface{}, 0, len(agentsList))
	for _, a := range agentsList {
		agentsOut = append(agentsOut, map[string]interface{}{
			"id":         a.ID,
			"x":          a.X,
			"y":          a.Y,
			"energy":     a.Energy,
			"age":        a.Age,
			"sex":        a.Sex,
			"spd":        a.Speed,
			"agg":        a.Aggression,
			"repro":      a.Repro,
			"exp":        a.Experience,
			"parents":    a.Parents,
			"strength":   a.Strength,
			"policy_dir": a.PolicyDir,
		})
	}
	foodsOut := make([]map[string]interface{}, 0, len(s.foods))
	for _, f := range s.foods {
		foodsOut = append(foodsOut, map[string]interface{}{"x": f.X, "y": f.Y, "energy": f.Energy})
	}
	sumAgg := 0.0
	for _, a := range agentsList {
		sumAgg += a.Aggression
	}
	avgAgg := 0.0
	if len(agentsList) > 0 {
		avgAgg = sumAgg / float64(len(agentsList))
	}
	avgLife := 0.0
	if s.totalDeaths > 0 {
		avgLife = float64(s.totalAgeAtDeath) / float64(s.totalDeaths)
	}
	metrics := map[string]interface{}{
		"population":     len(agentsList),
		"avg_energy":     s.avgEnergy(agentsList),
		"avg_aggression": avgAgg,
		"births":         s.totalBirths,
		"deaths":         s.totalDeaths,
		"avg_life":       avgLife,
	}

	eventsOut := make([]map[string]interface{}, 0, len(s.events))
	for _, e := range s.events {
		eventsOut = append(eventsOut, map[string]interface{}{
			"type":      e.Type,
			"actor_id":  e.ActorID,
			"actor_sex": e.ActorSex,
			"target_id": e.TargetID,
			"message":   e.Message,
		})
	}

	snapshot := map[string]interface{}{"type": "state", "agents": agentsOut, "foods": foodsOut, "metrics": metrics, "lineage": s.lineage, "events": eventsOut}

	select {
	case s.StateChan <- snapshot:
	default:
	}
}

func (s *Sim) avgEnergy(list []*Agent) float64 {
	if len(list) == 0 {
		return 0
	}
	sum := 0.0
	for _, a := range list {
		sum += a.Energy
	}
	return sum / float64(len(list))
}

func (s *Sim) chooseAction(a *Agent) ([]float64, []float64, int) {
	var fx, fy int
	found := false
	bestD := math.MaxFloat64
	for _, f := range s.foods {
		d := math.Abs(float64(f.X-a.X)) + math.Abs(float64(f.Y-a.Y))
		if d < bestD {
			bestD = d
			fx = f.X
			fy = f.Y
			found = true
		}
	}
	var dxNorm, dyNorm float64
	if found {
		dxNorm = float64(fx-a.X) / float64(s.W)
		dyNorm = float64(fy-a.Y) / float64(s.H)
	} else {
		dxNorm = 0
		dyNorm = 0
	}
	energyNorm := a.Energy / 100.0
	if energyNorm > 1 {
		energyNorm = 1
	}
	threat := 0.0
	for _, other := range s.agents {
		if other.ID == a.ID {
			continue
		}
		d := math.Abs(float64(other.X-a.X)) + math.Abs(float64(other.Y-a.Y))
		if d <= 3 {
			val := math.Max(0, other.Strength-a.Strength) / 10.0 * (1.0 / (d + 1.0))
			if val > threat {
				threat = val
			}
		}
	}
	if threat > 1 {
		threat = 1
	}
	features := []float64{1.0, dxNorm, dyNorm, energyNorm, threat}

	na := len(a.Weights)
	logits := make([]float64, na)
	maxl := -1e99
	for i := 0; i < na; i++ {
		sum := 0.0
		for j := 0; j < len(features); j++ {
			if j < len(a.Weights[i]) {
				sum += a.Weights[i][j] * features[j]
			}
		}
		logits[i] = sum
	}
	oldDist := s.distanceToNearestFood(a)
	biasScale := 3.0
	for i := 0; i < na; i++ {
		ddx := (i % 3) - 1
		ddy := (i / 3) - 1
		nx := clamp(a.X+ddx, 0, s.W-1)
		ny := clamp(a.Y+ddy, 0, s.H-1)
		best := math.MaxFloat64
		for _, f := range s.foods {
			d := math.Abs(float64(f.X-nx)) + math.Abs(float64(f.Y-ny))
			if d < best {
				best = d
			}
		}
		if best == math.MaxFloat64 {
			best = float64(s.W + s.H)
		}
		heuristic := (oldDist - best) * biasScale
		logits[i] += heuristic
		if logits[i] > maxl {
			maxl = logits[i]
		}
	}

	expsum := 0.0
	probs := make([]float64, na)
	for i := 0; i < na; i++ {
		v := math.Exp(logits[i] - maxl)
		probs[i] = v
		expsum += v
	}
	for i := 0; i < na; i++ {
		probs[i] /= expsum
	}

	r := s.rand.Float64()
	acc := 0.0
	act := 0
	for i := 0; i < na; i++ {
		acc += probs[i]
		if r <= acc {
			act = i
			break
		}
	}
	return features, probs, act
}

func dot(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	s := 0.0
	for i := 0; i < n; i++ {
		s += a[i] * b[i]
	}
	return s
}

func (s *Sim) computeFeaturesAndProbs(a *Agent) ([]float64, []float64) {
	var fx, fy int
	found := false
	bestD := math.MaxFloat64
	for _, f := range s.foods {
		d := math.Abs(float64(f.X-a.X)) + math.Abs(float64(f.Y-a.Y))
		if d < bestD {
			bestD = d
			fx = f.X
			fy = f.Y
			found = true
		}
	}
	var dxNorm, dyNorm float64
	if found {
		dxNorm = float64(fx-a.X) / float64(s.W)
		dyNorm = float64(fy-a.Y) / float64(s.H)
	} else {
		dxNorm = 0
		dyNorm = 0
	}
	energyNorm := a.Energy / 100.0
	if energyNorm > 1 {
		energyNorm = 1
	}
	threat := 0.0
	for _, other := range s.agents {
		if other.ID == a.ID {
			continue
		}
		d := math.Abs(float64(other.X-a.X)) + math.Abs(float64(other.Y-a.Y))
		if d <= 3 {
			val := math.Max(0, other.Strength-a.Strength) / 10.0 * (1.0 / (d + 1.0))
			if val > threat {
				threat = val
			}
		}
	}
	if threat > 1 {
		threat = 1
	}
	features := []float64{1.0, dxNorm, dyNorm, energyNorm, threat}

	na := len(a.Weights)
	logits := make([]float64, na)
	maxl := -1e99
	for i := 0; i < na; i++ {
		sum := 0.0
		for j := 0; j < len(features) && j < len(a.Weights[i]); j++ {
			sum += a.Weights[i][j] * features[j]
		}
		logits[i] = sum
		if sum > maxl {
			maxl = sum
		}
	}
	expsum := 0.0
	probs := make([]float64, na)
	for i := 0; i < na; i++ {
		v := math.Exp(logits[i] - maxl)
		probs[i] = v
		expsum += v
	}
	for i := 0; i < na; i++ {
		probs[i] /= expsum
	}
	return features, probs
}

func (s *Sim) updateActorCritic(a *Agent, features []float64, probs []float64, act int, reward float64) {
	alpha := a.REstAlpha
	if alpha <= 0 {
		alpha = 0.01
	}
	a.RMean = (1.0-alpha)*a.RMean + alpha*reward
	diff := reward - a.RMean
	a.RVar = (1.0-alpha)*a.RVar + alpha*diff*diff
	sigma := math.Sqrt(a.RVar)
	rhat := (reward - a.RMean) / (sigma + a.REps)

	V := dot(a.CriticW, features)
	nextFeatures, _ := s.computeFeaturesAndProbs(a)
	Vnext := dot(a.CriticW, nextFeatures)

	delta := rhat + a.Gamma*Vnext - V
	if delta > a.AdvClip {
		delta = a.AdvClip
	}
	if delta < -a.AdvClip {
		delta = -a.AdvClip
	}

	for j := 0; j < len(a.CriticW) && j < len(features); j++ {
		a.CriticW[j] += a.CriticLR * delta * features[j]
	}

	na := len(a.Weights)
	nf := len(features)
	for p := 0; p < na; p++ {
		factor := 0.0
		if p == act {
			factor = 1.0
		}
		factor -= probs[p]
		for j := 0; j < nf; j++ {
			a.Weights[p][j] += a.LearningRate * delta * factor * features[j]
		}
	}

	if a.EntropyBeta > 0 {
		for p := 0; p < na; p++ {
			lp := probs[p]
			if lp <= 0 {
				continue
			}
			coef := -(math.Log(lp) + 1.0) * lp
			for j := 0; j < nf; j++ {
				a.Weights[p][j] += a.EntropyBeta * coef * features[j]
			}
		}
	}
}

func (s *Sim) foodAt(x, y int) bool {
	_, ok := s.foods[x*s.H+y]
	return ok
}

func (s *Sim) foodAtKey(x, y int) (int, bool) {
	k := x*s.H + y
	_, ok := s.foods[k]
	return k, ok
}

func (s *Sim) agentAt(x, y int) bool {
	for _, a := range s.agents {
		if a.X == x && a.Y == y {
			return true
		}
	}
	return false
}

func (s *Sim) randomMove(a *Agent) {
	dx := s.rand.Intn(3) - 1
	dy := s.rand.Intn(3) - 1
	nx := clamp(a.X+dx, 0, s.W-1)
	ny := clamp(a.Y+dy, 0, s.H-1)
	a.X = nx
	a.Y = ny
}

func (s *Sim) distanceToNearestFood(a *Agent) float64 {
	best := math.MaxFloat64
	for _, f := range s.foods {
		d := math.Abs(float64(f.X-a.X)) + math.Abs(float64(f.Y-a.Y))
		if d < best {
			best = d
		}
	}
	if best == math.MaxFloat64 {
		return float64(s.W + s.H)
	}
	return best
}

func (s *Sim) moveTowardsFood(a *Agent) bool {
	sight := 6
	bestDist := math.MaxFloat64
	var fx, fy int
	found := false
	for _, f := range s.foods {
		d := math.Abs(float64(f.X-a.X)) + math.Abs(float64(f.Y-a.Y))
		if int(d) <= sight && d < bestDist {
			bestDist = d
			fx = f.X
			fy = f.Y
			found = true
		}
	}
	if !found {
		return false
	}
	dx := 0
	dy := 0
	if fx > a.X {
		dx = 1
	} else if fx < a.X {
		dx = -1
	}
	if fy > a.Y {
		dy = 1
	} else if fy < a.Y {
		dy = -1
	}
	nx := clamp(a.X+dx, 0, s.W-1)
	ny := clamp(a.Y+dy, 0, s.H-1)
	a.X = nx
	a.Y = ny
	return true
}

func (s *Sim) tryAttack(a *Agent) bool {
	if a.Sex == Female {
		return false
	}
	for _, other := range s.agents {
		if other.ID == a.ID {
			continue
		}
		if other.Sex == Female {
			continue
		}
		if abs(other.X-a.X)+abs(other.Y-a.Y) <= 1 {
			chance := (a.Aggression - other.Aggression) + (a.Strength-other.Strength)/10.0 + s.rand.NormFloat64()*0.2
			if chance > 0.1 {
				damage := 2 + s.rand.Float64()*3
				other.Energy -= damage
				a.Experience["attacks"]++
				a.Energy += damage * 0.1
				s.addEvent("attack", a.ID, a.Sex, other.ID, fmt.Sprintf("Агент %d (%s) атаковал %d (урон %.1f)", a.ID, a.Sex, other.ID, damage))
				if other.Energy <= 0 {
					s.totalDeaths++
					s.totalAgeAtDeath += other.Age
					s.addEvent("kill", a.ID, a.Sex, other.ID, fmt.Sprintf("Агент %d (%s) убил %d", a.ID, a.Sex, other.ID))
					delete(s.agents, other.ID)
					a.Experience["kills"]++
				}
				return true
			}
		}
	}
	return false
}

func (s *Sim) tryReproduce(a *Agent) bool {
	for _, other := range s.agents {
		if other.ID == a.ID {
			continue
		}
		if other.Sex == a.Sex {
			continue
		}
		if abs(other.X-a.X)+abs(other.Y-a.Y) <= 1 {
			if a.Energy > 15 && other.Energy > 15 && s.rand.Float64() < (a.Repro+other.Repro)/2 {

				s.nextID++
				child := &Agent{ID: s.nextID}
				child.X, child.Y = a.X, a.Y
				child.Energy = (a.Energy + other.Energy) / 4
				child.Sex = []Sex{Male, Female}[s.rand.Intn(2)]
				child.Age = 0
				child.Parents = []int{a.ID, other.ID}
				s.lineage[child.ID] = child.Parents
				s.totalBirths++
				child.Aggression = clampF((a.Aggression+other.Aggression)/2+(s.rand.NormFloat64()*0.05), 0, 1)
				child.Speed = clampInt((a.Speed+other.Speed)/2+int(s.rand.NormFloat64()*0.5), 1, 3)
				child.Repro = clampF((a.Repro+other.Repro)/2+s.rand.NormFloat64()*0.01, 0, 1)
				child.Experience = map[string]int{}
				child.Strength = (a.Strength+other.Strength)/2 + s.rand.NormFloat64()*0.5

				na := 9
				nf := 4
				if len(a.Weights) > 0 {
					na = len(a.Weights)
				}
				if len(a.LastState) > 0 {
					nf = len(a.LastState)
				}
				child.Weights = make([][]float64, na)
				for i := 0; i < na; i++ {
					child.Weights[i] = make([]float64, nf)
					for j := 0; j < nf; j++ {
						va := 0.0
						vb := 0.0
						if i < len(a.Weights) && j < len(a.Weights[i]) {
							va = a.Weights[i][j]
						}
						if i < len(other.Weights) && j < len(other.Weights[i]) {
							vb = other.Weights[i][j]
						}
						child.Weights[i][j] = (va+vb)/2 + s.rand.NormFloat64()*0.02
					}
				}
				child.LearningRate = (a.LearningRate + other.LearningRate) / 2
				child.LastState = make([]float64, nf)
				child.LastProbs = make([]float64, na)
				child.PolicyDir = 4
				child.Hunger = 0
				s.agents[child.ID] = child
				s.addEvent("birth", child.ID, child.Sex, 0, fmt.Sprintf("Рождён агент %d (%s) из %d и %d", child.ID, child.Sex, a.ID, other.ID))
				a.Energy *= 0.85
				other.Energy *= 0.85
				a.Experience["repro"]++
				other.Experience["repro"]++
				reproReward := 5.0
				a.RMean += reproReward
				other.RMean += reproReward
				return true
			}
		}
	}
	return false
}

func (s *Sim) tryMerge(a *Agent) bool {
	for _, other := range s.agents {
		if other.ID == a.ID {
			continue
		}
		if other.Sex != a.Sex {
			continue
		}
		if abs(other.X-a.X)+abs(other.Y-a.Y) <= 1 {
			combined := a.Energy + other.Energy
			mergeThreshold := 40.0
			baseProb := 0.15
			prob := baseProb + 0.2*(a.Repro+other.Repro)/2.0
			if combined > mergeThreshold && s.rand.Float64() < prob {
				oldAEnergy := a.Energy
				oldBEnergy := other.Energy
				costFactor := 0.85
				a.Energy = combined * costFactor

				wa := oldAEnergy / (combined + 1e-9)
				wb := oldBEnergy / (combined + 1e-9)
				a.Aggression = clampF(a.Aggression*wa+other.Aggression*wb, 0, 1)
				a.Strength = a.Strength*wa + other.Strength*wb + 0.5
				a.Repro = clampF(a.Repro*wa+other.Repro*wb, 0, 1)
				a.Speed = clampInt(max(a.Speed, other.Speed), 1, 5)
				for k, v := range other.Experience {
					a.Experience[k] += v
				}
				if len(other.Weights) > 0 {
					na := len(a.Weights)
					if len(other.Weights) > na {
						na = len(other.Weights)
					}
					nf := len(a.LastState)
					if len(other.LastState) > nf {
						nf = len(other.LastState)
					}
					newW := make([][]float64, na)
					for i := 0; i < na; i++ {
						newW[i] = make([]float64, nf)
						for j := 0; j < nf; j++ {
							va := 0.0
							vb := 0.0
							if i < len(a.Weights) && j < len(a.Weights[i]) {
								va = a.Weights[i][j]
							}
							if i < len(other.Weights) && j < len(other.Weights[i]) {
								vb = other.Weights[i][j]
							}
							newW[i][j] = va*wa + vb*wb
						}
					}
					a.Weights = newW
				}

				a.Parents = append(a.Parents, other.ID)
				s.lineage[a.ID] = a.Parents
				s.addEvent("merge", a.ID, a.Sex, other.ID, fmt.Sprintf("Агент %d (%s) слился с %d", a.ID, a.Sex, other.ID))
				delete(s.agents, other.ID)
				return true
			}
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v int, lo, hi int) int { return clamp(v, lo, hi) }

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (s *Sim) AddFoodAt(x, y int, energy float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x < 0 || x >= s.W || y < 0 || y >= s.H {
		return
	}
	key := x*s.H + y
	if s.foodAt(x, y) || s.agentAt(x, y) {
		return
	}
	s.foods[key] = &Food{X: x, Y: y, Energy: energy}
}

func (s *Sim) SetRandomFood(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RandomFood = enabled
}

func (s *Sim) AddAgentAt(x, y int, energy float64, sex Sex, aggression float64, speed int, strength float64, repro float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x < 0 || x >= s.W || y < 0 || y >= s.H {
		return
	}
	s.nextID++
	a := &Agent{
		ID:         s.nextID,
		X:          x,
		Y:          y,
		Energy:     energy,
		Sex:        sex,
		Age:        0,
		Aggression: clampF(aggression, 0, 1),
		Speed:      clampInt(speed, 1, 5),
		Strength:   strength,
		Repro:      clampF(repro*1.5, 0, 1),
		Experience: map[string]int{},
	}
	na := 9
	nf := 5
	a.Weights = make([][]float64, na)
	for i := 0; i < na; i++ {
		a.Weights[i] = make([]float64, nf)
		for j := 0; j < nf; j++ {
			a.Weights[i][j] = s.rand.NormFloat64() * 0.05
		}
	}
	a.LearningRate = 0.03
	a.LastState = make([]float64, nf)
	a.LastProbs = make([]float64, na)
	a.CriticW = make([]float64, nf)
	a.Gamma = 0.98
	a.CriticLR = 2 * a.LearningRate
	a.EntropyBeta = 0.01
	a.AdvClip = 6.0
	a.REstAlpha = 0.01
	a.REps = 1e-8
	a.PolicyDir = 4
	a.Hunger = 0
	s.agents[a.ID] = a
	s.totalBirths++
	s.lineage[a.ID] = []int{}
}
