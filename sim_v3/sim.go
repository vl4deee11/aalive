package simv3

import (
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

type StateKey struct {
	FoodDirection int `json:"food_dir"`
	FoodDistance  int `json:"food_dist"`
	EnergyLevel   int `json:"energy"`
	ThreatLevel   int `json:"threat"`
	NearbyAgents  int `json:"agents"`
	FatigueLevel  int `json:"fatigue"`
	HungerLevel   int `json:"hunger"`
	StressLevel   int `json:"stress"`
}

type Agent struct {
	ID            int            `json:"id"`
	X             int            `json:"x"`
	Y             int            `json:"y"`
	Energy        float64        `json:"energy"`
	Sex           Sex            `json:"sex"`
	Age           int            `json:"age"`
	Aggression    float64        `json:"agg"`
	Speed         int            `json:"spd"`
	Strength      float64        `json:"strength"`
	Repro         float64        `json:"repro"`
	Experience    map[string]int `json:"exp"`
	Parents       []int          `json:"parents"`
	SocialGroupID int            `json:"group_id"`
	Fatigue       float64        `json:"fatigue"`
	Hunger        float64        `json:"hunger"`
	Health        float64        `json:"health"`
	Stress        float64        `json:"stress"`

	QTable     map[StateKey]map[int]float64 `json:"-"`
	LastState  StateKey                     `json:"-"`
	LastAction int                          `json:"-"`
	Epsilon    float64                      `json:"-"`
	Alpha      float64                      `json:"-"`
	Gamma      float64                      `json:"-"`
	PolicyDir  int                          `json:"policy_dir"`

	mu sync.Mutex `json:"-"`
}

type SocialGroup struct {
	ID      int   `json:"id"`
	Members []int `json:"members"`
}

type Food struct {
	X      int     `json:"x"`
	Y      int     `json:"y"`
	Energy float64 `json:"energy"`
}

type Event struct {
	Type     string  `json:"type"`
	Tick     int     `json:"tick"`
	ActorID  int     `json:"actor_id"`
	TargetID *int    `json:"target_id,omitempty"`
	Value    float64 `json:"value,omitempty"`
}

type Sim struct {
	W, H         int
	agents       map[int]*Agent
	foods        map[int]*Food
	socialGroups map[int]*SocialGroup
	nextID       int

	StateChan chan interface{}
	mu        sync.Mutex
	rand      *rand.Rand
	ticks     int
	events    []Event

	isRunning bool
}

func NewAgent(id int, x, y int) *Agent {
	rand.Seed(time.Now().UnixNano())

	sex := Male
	if rand.Intn(2) == 0 {
		sex = Female
	}

	return &Agent{
		ID:            id,
		X:             x,
		Y:             y,
		Energy:        100.0,
		Sex:           sex,
		Age:           0,
		Aggression:    rand.Float64(),
		Speed:         rand.Intn(3) + 1,
		Strength:      rand.Float64() * 100,
		Repro:         rand.Float64(),
		Experience:    make(map[string]int),
		SocialGroupID: -1,
		Fatigue:       0.0,
		Hunger:        0.0,
		Health:        100.0,
		Stress:        0.0,
		QTable:        make(map[StateKey]map[int]float64),
		Epsilon:       0.1,
		Alpha:         0.1,
		Gamma:         0.9,
	}
}

func NewSim(w, h int) *Sim {
	s := &Sim{
		W:            w,
		H:            h,
		agents:       make(map[int]*Agent),
		foods:        make(map[int]*Food),
		socialGroups: make(map[int]*SocialGroup),
		StateChan:    make(chan interface{}, 100),
		rand:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for i := 0; i < 2; i++ {
		s.addRandomAgent()
	}
	return s
}

func (s *Sim) addRandomAgent() {
	x := s.rand.Intn(s.W)
	y := s.rand.Intn(s.H)
	s.addAgentAt(x, y, 100.0, Male, rand.Float64(), rand.Intn(3)+1, rand.Float64()*100, rand.Float64())
}

func (s *Sim) addAgentAt(x, y int, energy float64, sex Sex, aggression float64, speed int, strength float64, repro float64) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++
	a := NewAgent(id, x, y)
	a.Energy = energy
	a.Sex = sex
	a.Aggression = aggression
	a.Speed = speed
	a.Strength = strength
	a.Repro = repro
	s.agents[id] = a
	return id
}

func (s *Sim) AddAgentAt(x, y int, energy float64, sex Sex, aggression float64, speed int, strength float64, repro float64) int {
	return s.addAgentAt(x, y, energy, sex, aggression, speed, strength, repro)
}

func (s *Sim) AddFoodAt(x, y int, energy float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := x*s.H + y
	if _, exists := s.foods[key]; !exists {
		s.foods[key] = &Food{X: x, Y: y, Energy: energy}
	}
}

func (s *Sim) SetRandomFood(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Оставляем пустым, так как в v3 рандомная еда всегда включена
}

func (s *Sim) Run() {
	s.mu.Lock()
	s.isRunning = true
	s.mu.Unlock()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		if !s.isRunning {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		s.Tick()
	}
}

func (s *Sim) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ticks++

	attempts := (s.W * s.H) / 50
	for i := 0; i < attempts; i++ {
		if s.rand.Float64() < 0.04 {
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
		a.UpdatePhysiology()

		if a.Energy <= 0 {
			delete(s.agents, id)
			continue
		}

		currentState := a.UpdateState(s.getNearbyFoods(id), s.getNearbyAgents(id))
		action := a.ChooseAction(currentState)

		dx := (action % 3) - 1
		dy := (action / 3) - 1
		nx := clamp(a.X+dx, 0, s.W-1)
		a.X = nx
		a.Y = clamp(a.Y+dy, 0, s.H-1)

		if fkey, ok := s.foodAtKey(a.X, a.Y); ok {
			f := s.foods[fkey]
			a.Energy += f.Energy
			delete(s.foods, fkey)
			a.Hunger = 0
		}

		s.handleSocialInteractions(a)

		reward := s.calculateReward(a)
		a.QLearn(currentState, action, reward, a.UpdateState(s.getNearbyFoods(id), s.getNearbyAgents(id)))

		s.tryReproduce(a)
	}

	s.sendStateUpdate()
}

func (s *Sim) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isRunning = false
}

func (s *Sim) tryReproduce(a *Agent) {
	if a.Energy < 50 || a.Age < 30 || a.Repro < 0.1 {
		return
	}

	nearbyAgents := s.getNearbyAgents(a.ID)
	var partner *Agent
	for _, other := range nearbyAgents {
		if other.Sex != a.Sex && other.Energy > 40 && other.Age > 20 && other.Repro > 0.05 {
			partner = other
			break
		}
	}

	if partner == nil {
		return
	}

	if s.rand.Float64() < (a.Repro+partner.Repro)/2 {
		child := s.createChild(a, partner)
		s.agents[child.ID] = child
		a.Experience["repro"]++
		partner.Experience["repro"]++
		a.Energy -= 10
		partner.Energy -= 10
	}
}

func (s *Sim) createChild(parent1, parent2 *Agent) *Agent {
	id := s.nextID
	s.nextID++

	child := NewAgent(id, parent1.X, parent1.Y)
	child.Aggression = (parent1.Aggression + parent2.Aggression) / 2
	child.Speed = (parent1.Speed + parent2.Speed) / 2
	child.Strength = (parent1.Strength + parent2.Strength) / 2
	child.Repro = (parent1.Repro + parent2.Repro) / 2
	child.SocialGroupID = parent1.SocialGroupID

	child.applyMutations()

	child.Parents = []int{parent1.ID, parent2.ID}
	return child
}

func (a *Agent) applyMutations() {
	if rand.Float64() < 0.1 {
		a.Aggression += (rand.Float64() - 0.5) * 0.2
		if a.Aggression < 0 {
			a.Aggression = 0
		}
		if a.Aggression > 1 {
			a.Aggression = 1
		}
	}

	if rand.Float64() < 0.1 {
		a.Speed += rand.Intn(3) - 1
		if a.Speed < 1 {
			a.Speed = 1
		}
		if a.Speed > 5 {
			a.Speed = 5
		}
	}

	if rand.Float64() < 0.1 {
		a.Strength += (rand.Float64() - 0.5) * 20
		if a.Strength < 0 {
			a.Strength = 0
		}
		if a.Strength > 100 {
			a.Strength = 100
		}
	}

	if rand.Float64() < 0.1 {
		a.Repro += (rand.Float64() - 0.5) * 0.1
		if a.Repro < 0 {
			a.Repro = 0
		}
		if a.Repro > 1 {
			a.Repro = 1
		}
	}
}

func (s *Sim) getNearbyFoods(agentID int) []Food {
	a := s.agents[agentID]
	var foods []Food
	for _, food := range s.foods {
		dx := float64(food.X - a.X)
		dy := float64(food.Y - a.Y)
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < 50 {
			foods = append(foods, *food)
		}
	}
	return foods
}

func (s *Sim) getNearbyAgents(agentID int) []*Agent {
	a := s.agents[agentID]
	var agents []*Agent
	for id, other := range s.agents {
		if id == agentID {
			continue
		}
		dx := float64(other.X - a.X)
		dy := float64(other.Y - a.Y)
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < 20 {
			agents = append(agents, other)
		}
	}
	return agents
}

func (s *Sim) foodAt(x, y int) bool {
	key := x*s.H + y
	_, exists := s.foods[key]
	return exists
}

func (s *Sim) foodAtKey(x, y int) (int, bool) {
	key := x*s.H + y
	_, exists := s.foods[key]
	return key, exists
}

func (s *Sim) agentAt(x, y int) bool {
	for _, a := range s.agents {
		if a.X == x && a.Y == y {
			return true
		}
	}
	return false
}

func (s *Sim) sendStateUpdate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	agentsOut := make([]map[string]interface{}, 0, len(s.agents))
	for _, a := range s.agents {
		a.mu.Lock()
		agentData := map[string]interface{}{
			"id":         a.ID,
			"x":          a.X,
			"y":          a.Y,
			"energy":     a.Energy,
			"sex":        a.Sex,
			"age":        a.Age,
			"agg":        a.Aggression,
			"spd":        a.Speed,
			"strength":   a.Strength,
			"repro":      a.Repro,
			"exp":        a.Experience,
			"parents":    a.Parents,
			"group_id":   a.SocialGroupID,
			"fatigue":    a.Fatigue,
			"hunger":     a.Hunger,
			"health":     a.Health,
			"stress":     a.Stress,
			"policy_dir": a.PolicyDir,
		}
		agentsOut = append(agentsOut, agentData)
		a.mu.Unlock()
	}

	foodsOut := make([]map[string]interface{}, 0, len(s.foods))
	for _, f := range s.foods {
		foodData := map[string]interface{}{
			"x":      f.X,
			"y":      f.Y,
			"energy": f.Energy,
		}
		foodsOut = append(foodsOut, foodData)
	}

	snapshot := map[string]interface{}{
		"type":   "state",
		"agents": agentsOut,
		"foods":  foodsOut,
	}

	select {
	case s.StateChan <- snapshot:
	default:
	}
}

func (s *Sim) calculateReward(a *Agent) float64 {
	reward := 0.0

	reward += a.Energy / 100

	reward += float64(a.Age) / 1000

	reward -= a.Hunger / 200
	reward -= a.Fatigue / 200

	reward += float64(a.Experience["ate"]) / 100
	reward += float64(a.Experience["kills"]) / 50
	reward += float64(a.Experience["repro"]) / 20

	if a.SocialGroupID != -1 {
		reward += 0.2
	}

	reward -= a.Stress / 200

	return reward
}

func (s *Sim) handleSocialInteractions(a *Agent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	nearbyAgents := s.getNearbyAgents(a.ID)
	if len(nearbyAgents) == 0 {
		return
	}

	other := nearbyAgents[rand.Intn(len(nearbyAgents))]
	other.mu.Lock()
	defer other.mu.Unlock()

	if a.SocialGroupID == -1 && other.SocialGroupID == -1 {
		if rand.Float64() < 0.3 {
			newGroupID := rand.Intn(1000000)
			a.SocialGroupID = newGroupID
			other.SocialGroupID = newGroupID
			s.socialGroups[newGroupID] = &SocialGroup{
				ID:      newGroupID,
				Members: []int{a.ID, other.ID},
			}
		}
	} else if a.SocialGroupID != -1 && other.SocialGroupID == -1 {
		if rand.Float64() < 0.5 {
			other.SocialGroupID = a.SocialGroupID
			if group, exists := s.socialGroups[a.SocialGroupID]; exists {
				group.Members = append(group.Members, other.ID)
			}
		}
	} else if a.SocialGroupID == -1 && other.SocialGroupID != -1 {
		if rand.Float64() < 0.5 {
			a.SocialGroupID = other.SocialGroupID
			if group, exists := s.socialGroups[other.SocialGroupID]; exists {
				group.Members = append(group.Members, a.ID)
			}
		}
	} else if a.SocialGroupID == other.SocialGroupID {
		a.Energy += 0.5
		other.Energy += 0.5
		a.Stress -= 1.0
		other.Stress -= 1.0
	}

	if a.SocialGroupID != -1 && a.SocialGroupID == other.SocialGroupID {
		a.shareExperience(other)
	}
}

func (a *Agent) shareExperience(other *Agent) {
	for state, actions := range a.QTable {
		if _, exists := other.QTable[state]; !exists {
			other.QTable[state] = make(map[int]float64)
		}
		for action, q := range actions {
			if _, exists := other.QTable[state][action]; !exists {
				other.QTable[state][action] = q * 0.7
			}
		}
	}

	for state, actions := range other.QTable {
		if _, exists := a.QTable[state]; !exists {
			a.QTable[state] = make(map[int]float64)
		}
		for action, q := range actions {
			if _, exists := a.QTable[state][action]; !exists {
				a.QTable[state][action] = q * 0.7
			}
		}
	}
}

func (a *Agent) UpdateState(foods []Food, agents []*Agent) StateKey {
	a.mu.Lock()
	defer a.mu.Unlock()

	energyLevel := int(math.Floor(a.Energy / 100 * 4))
	if energyLevel > 4 {
		energyLevel = 4
	}

	fatigueLevel := int(math.Floor(a.Fatigue / 25))
	hungerLevel := int(math.Floor(a.Hunger / 33))
	stressLevel := int(math.Floor(a.Stress / 33))

	foodDir, foodDist := findNearestFood(a, foods)
	threatLevel, nearbyAgents := assessEnvironment(a, agents)

	// Динамическое изменение поведения
	a.adaptBehavior()

	return StateKey{
		FoodDirection: foodDir,
		FoodDistance:  foodDist,
		EnergyLevel:   energyLevel,
		ThreatLevel:   threatLevel,
		NearbyAgents:  nearbyAgents,
		FatigueLevel:  fatigueLevel,
		HungerLevel:   hungerLevel,
		StressLevel:   stressLevel,
	}
}

func (a *Agent) adaptBehavior() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.Age < 50 {
		a.Epsilon = 0.3
	} else if a.Age > 200 {
		a.Epsilon = 0.05
	}

	if a.Energy < 30 {
		a.Epsilon = 0.1
	}

	totalExperience := 0
	for _, exp := range a.Experience {
		totalExperience += exp
	}
	if totalExperience > 10 {
		a.Alpha = 0.15
	}
}

func (a *Agent) calculateBehaviorModifier() float64 {
	ageModifier := 1.0
	if a.Age < 50 {
		ageModifier = 0.8
	} else if a.Age > 200 {
		ageModifier = 1.2
	}

	energyModifier := a.Energy / 100

	experienceModifier := 1.0
	totalExperience := 0
	for _, exp := range a.Experience {
		totalExperience += exp
	}
	if totalExperience > 10 {
		experienceModifier = 1.5
	}

	return ageModifier * energyModifier * experienceModifier
}

func (a *Agent) UpdatePhysiology() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.Fatigue += 0.1 + rand.Float64()*0.2
	if a.Fatigue > 100 {
		a.Fatigue = 100
	}

	a.Hunger += 0.1 + rand.Float64()*0.3
	if a.Hunger > 100 {
		a.Hunger = 100
	}

	a.Stress += 0.05 + rand.Float64()*0.1
	if a.Stress > 100 {
		a.Stress = 100
	}

	healthImpact := (a.Fatigue + a.Hunger + a.Stress) / 600
	a.Health -= healthImpact
	if a.Health < 0 {
		a.Health = 0
	}

	a.Energy -= 0.08
}

func (a *Agent) QLearn(state StateKey, action int, reward float64, nextState StateKey) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.QTable[state]; !exists {
		a.QTable[state] = make(map[int]float64)
	}
	if _, exists := a.QTable[state][action]; !exists {
		a.QTable[state][action] = 0.0
	}
	if _, exists := a.QTable[nextState]; !exists {
		a.QTable[nextState] = make(map[int]float64)
	}

	maxNextQ := 0.0
	for _, q := range a.QTable[nextState] {
		if q > maxNextQ {
			maxNextQ = q
		}
	}

	currentQ := a.QTable[state][action]
	newQ := currentQ*(1-a.Alpha) + a.Alpha*(reward+a.Gamma*maxNextQ)
	a.QTable[state][action] = newQ
	a.adaptLearningParameters(reward)
}

func (a *Agent) adaptLearningParameters(reward float64) {
	if reward > 0.5 {
		a.Epsilon *= 0.995
	} else {
		a.Epsilon *= 0.999
	}
	if a.Epsilon < 0.01 {
		a.Epsilon = 0.01
	}

	if reward > 0.3 {
		a.Alpha = 0.15
	} else {
		a.Alpha = 0.05
	}
}

func (a *Agent) ChooseAction(state StateKey) int {
	a.mu.Lock()
	defer a.mu.Unlock()

	if rand.Float64() < a.Epsilon {
		return rand.Intn(9)
	}

	bestAction := 0
	bestQ := -math.MaxFloat64
	if actions, exists := a.QTable[state]; exists {
		for action, q := range actions {
			if q > bestQ {
				bestQ = q
				bestAction = action
			}
		}
	}
	return bestAction
}

func findNearestFood(a *Agent, foods []Food) (int, int) {
	minDist := math.MaxFloat64
	foodDir := 0
	foodDist := 4 // по умолчанию "нет еды"

	for _, food := range foods {
		dx := float64(food.X - a.X)
		dy := float64(food.Y - a.Y)
		dist := math.Sqrt(dx*dx + dy*dy)

		if dist < minDist {
			minDist = dist
			// Определяем направление (0-8)
			angle := math.Atan2(dy, dx) * 180 / math.Pi
			foodDir = int((angle+360)/45) % 8
			// Дискретизация дистанции
			if dist < 5 {
				foodDist = 0 // очень близко
			} else if dist < 15 {
				foodDist = 1 // близко
			} else if dist < 30 {
				foodDist = 2 // средне
			} else if dist < 60 {
				foodDist = 3 // далеко
			} else {
				foodDist = 4 // очень далеко
			}
		}
	}

	return foodDir, foodDist
}

func assessEnvironment(a *Agent, agents []*Agent) (int, int) {
	threatLevel := 0
	nearbyAgents := 0

	for _, other := range agents {
		dx := float64(other.X - a.X)
		dy := float64(other.Y - a.Y)
		dist := math.Sqrt(dx*dx + dy*dy)

		if dist < 10 {
			nearbyAgents++
			if other.Aggression > 0.7 && other.Strength > a.Strength {
				threatLevel = 2
			} else if other.Aggression > 0.5 {
				threatLevel = 1
			}
		}
	}

	return threatLevel, nearbyAgents
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
