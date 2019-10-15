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

// will keep messages in memory and whenever a channel closed, write it to a logfile in local disk
// name of log may include date and name of user to search later: this part is not implemented
var liveMessages map[string][]msgPost

// Since not utilizing DB for concurrency issues, RWMutex is preliminary solution per channel.
// Using DB will be significantly slow, so keep messages in memory and handle the critical regions
// since gorilla mux will kick goroutines(creates its concurrency) to handle each request
// only lock for the same channel and not using a global RWMutex to slow down:
var liveRWMutex map[string]*sync.RWMutex

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
	if liveRWMutex[channel] != nil {
		liveRWMutex[channel].RLock()
		defer liveRWMutex[channel].RUnlock()
		// Critical region
		messages, ok := liveMessages[channel]
		if ok {
			if id >= len(messages) {
				respondJSON(w, http.StatusBadRequest, "No new message after last_id")
				return
			}
			respondJSON(w, http.StatusOK, map[string][]msgPost{"messages": messages[id:]})
		} else {
			// This condition should not occur but left it just in case
			respondJSON(w, http.StatusBadRequest, "Sorry No such channel exist!")
		}
		// End of Critical region
	} else {
		respondJSON(w, http.StatusBadRequest, "Upps No such channel exist!")
	}

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

	if liveRWMutex[channel] != nil {
		liveRWMutex[channel].RLock()
		defer liveRWMutex[channel].RUnlock()
		// Critical region
		messages, ok := liveMessages[channel]
		if ok {
			if id >= len(messages) {
				respondJSON(w, http.StatusBadRequest, "No new message after last_id")
				return
			}
			respondJSON(w, http.StatusOK, map[string][]Thread{"messages": messages[id].Threads})
		} else {
			// This condition should not occur but left it just in case
			respondJSON(w, http.StatusBadRequest, "Sorry No such channel exist!")
		}
		// End of Critical region
	} else {
		respondJSON(w, http.StatusBadRequest, "Upps No such channel exist!")
	}

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
		// Add the new message and user into the corresponding channel
		// may need better concurrency solution here
		if liveRWMutex[channel] == nil {
			// Initialize Read write mutex only Once
			// This could be better to do in thread creation but in this quick
			// implementation done here to provide thread safety
			globalMutex.Lock()
			defer globalMutex.Unlock()
			if liveRWMutex[channel] == nil {
				liveRWMutex[channel] = &sync.RWMutex{}
			}
		}
		var id int
		{
			// Begining of critical region
			liveRWMutex[channel].Lock()
			defer liveRWMutex[channel].Unlock()
			id = len(liveMessages[channel])
			id++ // increment id and update
			mesg.Id = id
			liveMessages[channel] = append(liveMessages[channel], mesg)
			// End of critical region
		}

		respondJSON(w, http.StatusOK, map[string]int{"id": id})
	} else {
		respondJSON(w, http.StatusBadRequest, "Empty username or message!")
	}

}

// curl -X POST http://localhost:8000/gdgsas022/messages -d '{"username": "arthur", "message": "How are you"}' -v
// curl -X POST http://localhost:8000/gdgsas022/messages -d '{"username": "sandy", "message": "sdasdh lsdhsalhdlahdlshdld"}' -v
// curl -X GET http://localhost:8000/thread/gdgsas022/1 -v
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
		if liveRWMutex[channel] == nil {
			// there is no channel for this thread
			respondJSON(w, http.StatusBadRequest, "Provided channel does not exist!")
			return
		}
		{
			// Begining of critical region
			liveRWMutex[channel].Lock()
			defer liveRWMutex[channel].Unlock()
			liveMessages[channel][id].Threads = append(liveMessages[channel][id].Threads, mesg)
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
	liveMessages = make(map[string][]msgPost)
	liveRWMutex = make(map[string]*sync.RWMutex)

	router.HandleFunc("/{channel:[A-Z,a-z,0-9,-]+}/messages", getMessage).Methods("GET")
	router.HandleFunc("/{channel:[A-Z,a-z,0-9,-]+}/messages", postMessage).Methods("POST")
	router.HandleFunc("/{channel:[A-Z,a-z,0-9,-]+}/thread/{message_id}", getThreads).Methods("GET")
	router.HandleFunc("/{channel:[A-Z,a-z,0-9,-]+}/thread/{message_id}", postThread).Methods("POST")
	err := http.ListenAndServe(port, router)
	if err != nil {
		panic(err)
	}
}
