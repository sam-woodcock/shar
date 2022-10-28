package main

import (
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

func newIndex(store string) error {

	var (
		e     error
		index bleve.Index
	)

	mapping := bleve.NewIndexMapping()
	if index, e = bleve.New(store, mapping); e != nil {
		return e
	}

	if e = index.Close(); e != nil {
		fmt.Println(e)
	}

	return nil
}

func addKvJson(store string, kvjson map[string]string) error {

	var (
		e         error
		key, json string
		index     bleve.Index
	)

	if index, e = bleve.Open(store); e != nil {
		return e
	}

	for key, json = range kvjson {
		fmt.Println(key, json)
		if e = index.Index(key, json); e != nil {
			fmt.Println(e)
		}
	}

	if e = index.Close(); e != nil {
		fmt.Println(e)
	}

	return nil
}

func valueMatch(store, pattern string) error {

	var (
		e       error
		index   bleve.Index
		query   *query.MatchQuery
		search  *bleve.SearchRequest
		results *bleve.SearchResult
	)

	if index, e = bleve.Open(store); e != nil {
		return e
	}

	query = bleve.NewMatchQuery(pattern)
	search = bleve.NewSearchRequest(query)
	if results, e = index.Search(search); e != nil {
		return e
	}

	fmt.Println(results)

	return nil
}
