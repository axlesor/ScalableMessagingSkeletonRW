package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/mux"
)

// Each message can have multiple sub conversations
type Thread struct {
	Username string `json:"username"`
	Message  string `json:"message"`
}

type msgPost struct {
	Id       int      `json:"id"`
	Username string   `json:"username"`
	Message  string   `json:"message"`
	Threads  []Thread `json:"thread"`
}

type subject struct {
	sync.RWMutex
	Messages []msgPost
	title    string
}

// will keep messages in memory and whenever a channel closed, write it to a logfile in local disk
// name of log may include date and name of user to search later: this part is not implemented
var liveMessages map[string]*subject

// Since not utilizing DB for concurrency issues, RWMutex is preliminary solution per channel.
// Using DB will be significantly slow, so keep messages in memory and handle the critical regions
// since gorilla mux will kick goroutines(creates its concurrency) to handle each request
// only lock for the same channel and do not use a global RWMutex to slow down.
// Need globalMutex only for initial creation of subject for each channel
var globalMutex sync.Mutex

func getMessage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	channel := strings.ToLower(vars["channel"])
	//fmt.Printf("Messaging Get Endpoint ch: %s\n", channel)
	key := r.URL.Query().Get("last_id")
	var id int
	if key == "" {
		id = 0
	} else {
		var err error
		id, err = strconv.Atoi(key)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, "last_id should be an integer")
			return
		}
	}

	subject, ok := liveMessages[channel]
	if ok {
		// Critical region
		subject.RLock()
		defer subject.RUnlock()
		if id >= len(subject.Messages) {
			respondJSON(w, http.StatusBadRequest, "No new message after last_id")
			return
		}
		respondJSON(w, http.StatusOK, map[string][]msgPost{"messages": subject.Messages[id:]})
	} else {
		// Channel do not exist
		respondJSON(w, http.StatusBadRequest, "Sorry No such channel exist!")
	}
	// End of Critical region

}

func getThreads(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	channel := strings.ToLower(vars["channel"])
	//fmt.Printf("Messaging Get Endpoint ch: %s\n", channel)
	id, err := strconv.Atoi(vars["message_id"])
	if err != nil {
		respondJSON(w, http.StatusBadRequest, "message_id should be an integer")
		return
	}

	subject, ok := liveMessages[channel]
	if ok {
		// Critical region
		subject.RLock()
		defer subject.RUnlock()
		if id >= len(subject.Messages) {
			respondJSON(w, http.StatusBadRequest, "No message for the provided id")
			return
		}
		respondJSON(w, http.StatusOK, map[string][]Thread{"messages": subject.Messages[id].Threads})
	} else {
		respondJSON(w, http.StatusBadRequest, "Sorry No such channel exist!")
	}
	// End of Critical region

}

// tested using curl:
// curl -X POST http://localhost:8000/gdgsas022/messages -d '{"username": "Arthur", "message": "How are you"}' -v
// curl -X GET http://localhost:8000/gasli345/messages -v
// curl -X GET http://localhost:8000/gasli345/messages?last_id=2 -v
func postMessage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	channel := vars["channel"]
	//fmt.Printf("Message Post received on channel: %s\n", channel)

	mesg := msgPost{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()
	if err := decoder.Decode(&mesg); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	//fmt.Printf("Received: %+v\n", mesg)

	if mesg.Username != "" && mesg.Message != "" {
		// If it is the first time than create subject for the channel
		// may use better concurrency solution here!
		if liveMessages[channel] == nil {
			// Initialize Subject only Once
			// This could be better to do in subject creation: in this quick
			// implementation done here to provide thread safety
			globalMutex.Lock()
			defer globalMutex.Unlock()
			// Double checking to make sure no two threads come here at the same time
			if liveMessages[channel] == nil {
				liveMessages[channel] = &subject{}
			}
		}
		var id int
		{
			// Begining of critical region, get Write mutex
			liveMessages[channel].Lock()
			defer liveMessages[channel].Unlock()
			id = len(liveMessages[channel].Messages)
			id++ // increment id and update
			mesg.Id = id
			liveMessages[channel].Messages = append(liveMessages[channel].Messages, mesg)
			// End of critical region
		}

		respondJSON(w, http.StatusOK, map[string]int{"id": id})
	} else {
		respondJSON(w, http.StatusBadRequest, "Empty username or message!")
	}

}

// curl -X POST http://localhost:8000/gdgsas022/messages -d '{"username": "arthur", "message": "How are you"}' -v
// curl -X POST http://localhost:8000/gdgsas022/messages -d '{"username": "sandy", "message": "sdasdh lsdhsalhdlahdlshdld"}' -v
// curl  POST http://localhost:8000/gdgsas022/thread/1 -d '{"username": "sally", "message": "Nice sldhsld asdhasfh fhsf hdahdahdhas"}' -v
// curl -X GET http://localhost:8000/gdgsas022/thread/1 -v
func postThread(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	channel := vars["channel"]
	//fmt.Printf("Message Post received on channel: %s\n", channel)
	id, err := strconv.Atoi(vars["message_id"])
	if err != nil {
		respondJSON(w, http.StatusBadRequest, "message_id should be an integer")
		return
	}

	mesg := Thread{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()
	if err := decoder.Decode(&mesg); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	//fmt.Printf("Received: %+v\n", mesg)

	if mesg.Username != "" && mesg.Message != "" {
		// Add the new message and user into the corresponding channel
		// may need better concurrency solution here
		if liveMessages[channel] == nil {
			// no channel for this thread
			respondJSON(w, http.StatusBadRequest, "Provided channel does not exist!")
			return
		}
		{
			// make sure message id is valid
			if id >= len(liveMessages[channel].Messages) {
				respondJSON(w, http.StatusBadRequest, "Provided messageId does not exist!")
				return
			}
			// Begining of critical region
			liveMessages[channel].Lock()
			defer liveMessages[channel].Unlock()
			liveMessages[channel].Messages[id].Threads = append(liveMessages[channel].Messages[id].Threads, mesg)
			// End of critical region
		}

		respondJSON(w, http.StatusOK, map[string]int{"id": id})
	} else {
		respondJSON(w, http.StatusBadRequest, "Empty username or message!")
	}

}

func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(response))
}

func respondError(w http.ResponseWriter, code int, message string) {
	respondJSON(w, code, map[string]string{"error": message})
}

func main() {
	port := ":8000"
	fmt.Println("Messaging Service v0.01 started at port ", port)
	router := mux.NewRouter()
	// Messages will be stored according to their channel
	liveMessages = make(map[string]*subject)

	router.HandleFunc("/{channel:[A-Z,a-z,0-9,-]+}/messages", getMessage).Methods("GET")
	router.HandleFunc("/{channel:[A-Z,a-z,0-9,-]+}/messages", postMessage).Methods("POST")
	router.HandleFunc("/{channel:[A-Z,a-z,0-9,-]+}/thread/{message_id}", getThreads).Methods("GET")
	router.HandleFunc("/{channel:[A-Z,a-z,0-9,-]+}/thread/{message_id}", postThread).Methods("POST")
	err := http.ListenAndServe(port, router)
	if err != nil {
		panic(err)
	}
}
