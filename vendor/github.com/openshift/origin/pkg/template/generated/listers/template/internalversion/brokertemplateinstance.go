// Code generated by lister-gen. DO NOT EDIT.

package internalversion

import (
	template "github.com/openshift/origin/pkg/template/apis/template"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// BrokerTemplateInstanceLister helps list BrokerTemplateInstances.
type BrokerTemplateInstanceLister interface {
	// List lists all BrokerTemplateInstances in the indexer.
	List(selector labels.Selector) (ret []*template.BrokerTemplateInstance, err error)
	// Get retrieves the BrokerTemplateInstance from the index for a given name.
	Get(name string) (*template.BrokerTemplateInstance, error)
	BrokerTemplateInstanceListerExpansion
}

// brokerTemplateInstanceLister implements the BrokerTemplateInstanceLister interface.
type brokerTemplateInstanceLister struct {
	indexer cache.Indexer
}

// NewBrokerTemplateInstanceLister returns a new BrokerTemplateInstanceLister.
func NewBrokerTemplateInstanceLister(indexer cache.Indexer) BrokerTemplateInstanceLister {
	return &brokerTemplateInstanceLister{indexer: indexer}
}

// List lists all BrokerTemplateInstances in the indexer.
func (s *brokerTemplateInstanceLister) List(selector labels.Selector) (ret []*template.BrokerTemplateInstance, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*template.BrokerTemplateInstance))
	})
	return ret, err
}

// Get retrieves the BrokerTemplateInstance from the index for a given name.
func (s *brokerTemplateInstanceLister) Get(name string) (*template.BrokerTemplateInstance, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(template.Resource("brokertemplateinstance"), name)
	}
	return obj.(*template.BrokerTemplateInstance), nil
}
