package nsfContext

import (
	"encoding/json"
	"fmt"
	"sync"

	"nsf/internal/db"
	"nsf/logger"
)

// serviceInfo is the information of the NF which provides a service.
// It is stored in the DB as a JSON string with the service name as the key.
type serviceInfo struct {
	NfName     string `json:"nf_name"`
	SbiAddress string `json:"sbi_address"`
	SbiPort    int    `json:"sbi_port"`
}

type serviceContextIE struct {
	db.DbIf

	*logger.NsfLogger
}

type serviceContext struct {
	db.DbIf

	// services is the in-memory cache of the DB: service name -> NF information.
	// mu protects services, and also keeps the cache and the DB updated together.
	mu       sync.RWMutex
	services map[string]serviceInfo

	*logger.NsfLogger
}

func newServiceContext(serviceContextIE *serviceContextIE) *serviceContext {
	s := &serviceContext{
		DbIf: serviceContextIE.DbIf,

		services: make(map[string]serviceInfo),

		NsfLogger: serviceContextIE.NsfLogger,
	}

	// NSF recovery: rebuild the cache from the services stored in the DB,
	// so the services registered before a restart can still be discovered.
	s.recover()

	return s
}

func (s *serviceContext) recover() {
	data, err := s.DbIf.LoadAll()
	if err != nil {
		s.NsfLogger.SvcLog.Errorf("Failed to load services from DB: %v", err)
		return
	}

	for service, value := range data {
		var info serviceInfo
		if err := json.Unmarshal([]byte(value), &info); err != nil {
			s.NsfLogger.SvcLog.Warnf("Skip invalid service data of %s in DB: %v", service, err)
			continue
		}
		s.services[service] = info
	}

	s.NsfLogger.SvcLog.Infof("Recovered %d services from DB", len(s.services))
}

func (s *serviceContext) release() {
	s.NsfLogger.SvcLog.Infoln("Release serviceContext...")

	// Only clear the cache. The services stay in the DB for recovery,
	// and the DB itself is closed by dbContext.
	s.mu.Lock()
	s.services = make(map[string]serviceInfo)
	s.mu.Unlock()

	s.NsfLogger.SvcLog.Infoln("serviceContext released")
}

func (s *serviceContext) registerServices(nfName, sbiAddr string, sbiPort int, services []string) error {
	s.NsfLogger.SvcLog.Tracef("Registering %s services", nfName)

	info := serviceInfo{
		NfName:     nfName,
		SbiAddress: sbiAddr,
		SbiPort:    sbiPort,
	}
	value, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to encode service information of %s: %v", nfName, err)
	}

	data := make(map[string]string, len(services))
	for _, service := range services {
		data[service] = string(value)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Write the DB first, and only update the cache when the DB write succeeds.
	if err := s.DbIf.SaveAll(data); err != nil {
		return fmt.Errorf("failed to save services of %s: %v", nfName, err)
	}

	for _, service := range services {
		if old, ok := s.services[service]; ok && old.NfName != nfName {
			s.NsfLogger.SvcLog.Warnf("Service %s is taken over from %s by %s", service, old.NfName, nfName)
		}
		s.services[service] = info
	}

	s.NsfLogger.SvcLog.Infof("Registered %s services: %v", nfName, services)
	return nil
}

func (s *serviceContext) deregisterServices(nfName string, services []string) error {
	s.NsfLogger.SvcLog.Tracef("De-registering %s services", nfName)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check every service before removing anything, so a bad request removes nothing.
	for _, service := range services {
		if info, ok := s.services[service]; ok && info.NfName != nfName {
			return fmt.Errorf("service %s is registered by %s, not %s", service, info.NfName, nfName)
		}
	}

	for _, service := range services {
		if _, ok := s.services[service]; !ok {
			s.NsfLogger.SvcLog.Warnf("Service %s is not registered, skip it", service)
			continue
		}
		if err := s.DbIf.Remove(service); err != nil {
			return fmt.Errorf("failed to remove service %s of %s: %v", service, nfName, err)
		}
		delete(s.services, service)
	}

	s.NsfLogger.SvcLog.Infof("De-registered %s services: %v", nfName, services)
	return nil
}

func (s *serviceContext) discoverService(service string) (string, string, int, error) {
	s.NsfLogger.SvcLog.Tracef("Discovering service %s", service)

	s.mu.RLock()
	defer s.mu.RUnlock()

	info, ok := s.services[service]
	if !ok {
		return "", "", 0, fmt.Errorf("service %s not found", service)
	}

	return info.NfName, info.SbiAddress, info.SbiPort, nil
}
