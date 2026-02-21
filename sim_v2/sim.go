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

// StateKey представляет дискретизированное состояние среды
type StateKey struct {
	FoodDirection int // 0-8: направление к ближайшей еде
	FoodDistance  int // 0-4: дистанция до еды (близко, средне, далеко, очень далеко, нет)
	EnergyLevel   int // 0-4: уровень энергии
	ThreatLevel   int // 0-3: уровень угрозы
	NearbyAgents  int // 0-3: количество соседних агентов
}

type Agent struct {
	ID         int            `json:"id"`
	X          int            `json:"x"`
	Y          int            `json:"y"`
	Energy     float64        `json:"energy"`
	Sex        Sex            `json:"sex"`
	Age        int            `json:"age"`
	Aggression float64        `json:"agg"`
	Speed      int            `json:"spd"`
	Strength   float64        `json:"strength"`
	Repro      float64        `json:"repro"`
	Experience map[string]int `json:"exp"`
	Parents    []int          `json:"parents"`
	Hunger     int            `json:"-"`

	QTable     map[StateKey]map[int]float64 `json:"-"`
	LastState  StateKey                     `json:"-"`
	LastAction int                          `json:"-"`
	Epsilon    float64                      `json:"-"`
	Alpha      float64                      `json:"-"`
	Gamma      float64                      `json:"-"`
	PolicyDir  int                          `json:"policy_dir"`
}

type Food struct {
	X      int     `json:"x"`
	Y      int     `json:"y"`
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
		Parents:    []int{},
		Hunger:     0,

		QTable:    make(map[StateKey]map[int]float64),
		Epsilon:   0.3,
		Alpha:     0.1,
		Gamma:     0.95,
		PolicyDir: 4,
	}

	s.initializeQTable(a)

	s.agents[a.ID] = a
	s.totalBirths++
	s.lineage[a.ID] = []int{}
}

func (s *Sim) initializeQTable(a *Agent) {
	actions := 9

	for foodDir := 0; foodDir < 9; foodDir++ {
		for foodDist := 0; foodDist < 5; foodDist++ {
			for energy := 0; energy < 5; energy++ {
				for threat := 0; threat < 4; threat++ {
					for agents := 0; agents < 4; agents++ {
						state := StateKey{
							FoodDirection: foodDir,
							FoodDistance:  foodDist,
							EnergyLevel:   energy,
							ThreatLevel:   threat,
							NearbyAgents:  agents,
						}
						a.QTable[state] = make(map[int]float64)
						for action := 0; action < actions; action++ {
							a.QTable[state][action] = s.rand.Float64()*0.1 - 0.05
						}
					}
				}
			}
		}
	}
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
		oldDist := s.distanceToNearestFood(a)

		currentState := s.getStateKey(a)
		action := s.chooseAction(a, currentState)

		a.LastState = currentState
		a.LastAction = action
		a.PolicyDir = action

		dx := (action % 3) - 1
		dy := (action / 3) - 1
		nx := clamp(a.X+dx, 0, s.W-1)
		ny := clamp(a.Y+dy, 0, s.H-1)
		a.X = nx
		a.Y = ny
		newDist := s.distanceToNearestFood(a)

		if fkey, ok := s.foodAtKey(a.X, a.Y); ok {
			f := s.foods[fkey]
			a.Energy += f.Energy
			delete(s.foods, fkey)
			a.Experience["ate"]++
			a.Hunger = 0
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
		reward += (oldDist - newDist) * 1.5
		if a.Energy > 50 {
			reward += 0.1
		}
		s.updateQTable(a, reward)
		if a.Epsilon > 0.01 {
			a.Epsilon *= 0.9995
		}
	}
	s.sendStateUpdate()
}

func (s *Sim) getStateKey(a *Agent) StateKey {
	foodX, foodY := -1, -1
	bestDist := math.MaxFloat64
	for _, f := range s.foods {
		d := math.Abs(float64(f.X-a.X)) + math.Abs(float64(f.Y-a.Y))
		if d < bestDist {
			bestDist = d
			foodX, foodY = f.X, f.Y
		}
	}

	// Направление к еде (0-8)
	foodDir := 8
	if foodX >= 0 {
		dx := clampInt(foodX-a.X, -1, 1) + 1
		dy := clampInt(foodY-a.Y, -1, 1) + 1
		foodDir = dy*3 + dx
	}

	// Дистанция до еды
	foodDist := 4 // очень далеко/нет еды
	if bestDist < math.MaxFloat64 {
		if bestDist <= 2 {
			foodDist = 0 // очень близко
		} else if bestDist <= 5 {
			foodDist = 1 // близко
		} else if bestDist <= 10 {
			foodDist = 2 // средне
		} else if bestDist <= 20 {
			foodDist = 3 // далеко
		}
	}

	energyLevel := 0
	if a.Energy > 150 {
		energyLevel = 4
	} else if a.Energy > 100 {
		energyLevel = 3
	} else if a.Energy > 50 {
		energyLevel = 2
	} else if a.Energy > 25 {
		energyLevel = 1
	}

	threatLevel := 0
	for _, other := range s.agents {
		if other.ID == a.ID || other.Sex == Female {
			continue
		}
		d := math.Abs(float64(other.X-a.X)) + math.Abs(float64(other.Y-a.Y))
		if d <= 2 {
			threatLevel++
		}
	}
	if threatLevel > 3 {
		threatLevel = 3
	}

	nearbyAgents := 0
	for _, other := range s.agents {
		if other.ID == a.ID {
			continue
		}
		d := math.Abs(float64(other.X-a.X)) + math.Abs(float64(other.Y-a.Y))
		if d <= 3 {
			nearbyAgents++
		}
	}
	if nearbyAgents > 3 {
		nearbyAgents = 3
	}

	return StateKey{
		FoodDirection: foodDir,
		FoodDistance:  foodDist,
		EnergyLevel:   energyLevel,
		ThreatLevel:   threatLevel,
		NearbyAgents:  nearbyAgents,
	}
}

func (s *Sim) chooseAction(a *Agent, state StateKey) int {
	if s.rand.Float64() < a.Epsilon {
		return s.rand.Intn(9)
	}

	qValues := a.QTable[state]
	bestAction := 4
	bestValue := qValues[bestAction]

	for action, value := range qValues {
		if value > bestValue {
			bestValue = value
			bestAction = action
		}
	}

	return bestAction
}

func (s *Sim) updateQTable(a *Agent, reward float64) {
	if a.LastAction == -1 {
		return
	}

	nextState := s.getStateKey(a)

	// Q(s,a) = Q(s,a) + α * (r + γ * max(Q(s',a')) - Q(s,a))
	oldQ := a.QTable[a.LastState][a.LastAction]

	maxNextQ := 0.0
	if nextQValues, exists := a.QTable[nextState]; exists {
		for _, value := range nextQValues {
			if value > maxNextQ {
				maxNextQ = value
			}
		}
	}

	newQ := oldQ + a.Alpha*(reward+a.Gamma*maxNextQ-oldQ)
	a.QTable[a.LastState][a.LastAction] = newQ
}

func (s *Sim) sendStateUpdate() {
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
				child.Hunger = 0

				child.QTable = make(map[StateKey]map[int]float64)
				child.Epsilon = (a.Epsilon + other.Epsilon) / 2
				child.Alpha = (a.Alpha + other.Alpha) / 2
				child.Gamma = (a.Gamma + other.Gamma) / 2
				child.PolicyDir = 4

				for state, qValues := range a.QTable {
					child.QTable[state] = make(map[int]float64)
					for action, value := range qValues {
						parentValue := value
						otherValue := 0.0
						if otherQValues, exists := other.QTable[state]; exists {
							if val, exists := otherQValues[action]; exists {
								otherValue = val
							}
						}
						child.QTable[state][action] = (parentValue+otherValue)/2 + s.rand.NormFloat64()*0.02
					}
				}

				for state, qValues := range other.QTable {
					if _, exists := child.QTable[state]; !exists {
						child.QTable[state] = make(map[int]float64)
						for action, value := range qValues {
							child.QTable[state][action] = value + s.rand.NormFloat64()*0.02
						}
					}
				}

				s.agents[child.ID] = child
				s.addEvent("birth", child.ID, child.Sex, 0, fmt.Sprintf("Рождён агент %d (%s) из %d и %d", child.ID, child.Sex, a.ID, other.ID))
				a.Energy *= 0.85
				other.Energy *= 0.85
				a.Experience["repro"]++
				other.Experience["repro"]++
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

				for state, qValues := range other.QTable {
					if aQValues, exists := a.QTable[state]; exists {
						for action, value := range qValues {
							if aValue, exists := aQValues[action]; exists {
								a.QTable[state][action] = aValue*wa + value*wb
							} else {
								a.QTable[state][action] = value
							}
						}
					} else {
						a.QTable[state] = make(map[int]float64)
						for action, value := range qValues {
							a.QTable[state][action] = value
						}
					}
				}

				a.Epsilon = a.Epsilon*wa + other.Epsilon*wb
				a.Alpha = a.Alpha*wa + other.Alpha*wb
				a.Gamma = a.Gamma*wa + other.Gamma*wb

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
		Parents:    []int{},
		Hunger:     0,

		QTable:    make(map[StateKey]map[int]float64),
		Epsilon:   0.3,
		Alpha:     0.1,
		Gamma:     0.95,
		PolicyDir: 4,
	}

	s.initializeQTable(a)
	s.agents[a.ID] = a
	s.totalBirths++
	s.lineage[a.ID] = []int{}
}
