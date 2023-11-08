// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpointsv2

import (
	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/go-uuid"
	"testing"
)

func Test_writeCache(t *testing.T) {
	testHash := randomBytes()
	testGeneration := randomString()
	testK8sUid := randomString()

	type args struct {
		key               string
		hash              []byte
		generationFetchFn func() string
		k8sUid            string
	}
	cases := []struct {
		name    string
		args    args
		setupFn func(args args, cache WriteCache)
		want    bool
	}{
		{
			name: "No data returns false",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			want: false,
		},
		{
			name: "Non-matching key returns false",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update("another-key", args.hash, args.generationFetchFn(), args.k8sUid)
			},
			want: false,
		},
		{
			name: "Non-matching hash returns false",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update(args.key, randomBytes(), args.generationFetchFn(), args.k8sUid)
			},
			want: false,
		},
		{
			name: "Non-matching generation returns false",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update(args.key, args.hash, randomString(), args.k8sUid)
			},
			want: false,
		},
		{
			name: "Non-matching k8sUid returns false",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update(args.key, args.hash, args.generationFetchFn(), randomString())
			},
			want: false,
		},
		{
			name: "Matching data returns true",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update(args.key, args.hash, args.generationFetchFn(), args.k8sUid)
			},
			want: true,
		},
		{
			name: "Removed data returns false",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update(args.key, args.hash, args.generationFetchFn(), args.k8sUid)
				cache.update("another-key", randomBytes(), randomString(), randomString())
				cache.remove(args.key)
			},
			want: false,
		},
		{
			name: "Replaced data returns false",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update(args.key, args.hash, args.generationFetchFn(), args.k8sUid)
				cache.update(args.key, randomBytes(), args.generationFetchFn(), args.k8sUid)
			},
			want: false,
		},
		{
			name: "Invalid hash does not update cache",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update(args.key, args.hash, args.generationFetchFn(), args.k8sUid)
				cache.update(args.key, []byte{}, args.generationFetchFn(), args.k8sUid)
			},
			want: true,
		},
		{
			name: "Invalid generation does not update cache",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update(args.key, args.hash, args.generationFetchFn(), args.k8sUid)
				cache.update(args.key, args.hash, "", args.k8sUid)
			},
			want: true,
		},
		{
			name: "Invalid k8sUid does not update cache",
			args: args{
				"foo",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update(args.key, args.hash, args.generationFetchFn(), args.k8sUid)
				cache.update(args.key, args.hash, args.generationFetchFn(), "")
			},
			want: true,
		},
		{
			name: "Invalid key is ignored",
			args: args{
				"",
				testHash,
				func() string {
					return testGeneration
				},
				testK8sUid,
			},
			setupFn: func(args args, cache WriteCache) {
				cache.update("", args.hash, args.generationFetchFn(), args.k8sUid)
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewWriteCache(logrtest.New(t))
			if tc.setupFn != nil {
				tc.setupFn(tc.args, c)
			}
			if got := c.hasMatch(tc.args.key, tc.args.hash, tc.args.generationFetchFn, tc.args.k8sUid); got != tc.want {
				t.Errorf("hasMatch() = %v, want %v", got, tc.want)
			}
		})
	}
}

func randomBytes() []byte {
	b, err := uuid.GenerateRandomBytes(32)
	if err != nil {
		panic(err)
	}
	return b
}

func randomString() string {
	u, err := uuid.GenerateUUID()
	if err != nil {
		panic(err)
	}
	return u
}
