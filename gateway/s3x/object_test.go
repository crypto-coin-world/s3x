package s3x

import (
	"bytes"
	"context"
	"io"
	"math"
	"testing"

	minio "github.com/minio/minio/cmd"
	"github.com/minio/minio/pkg/hash"
)

const (
	testObject1     = "testobject1"
	testObject1Data = "testobject1data"
)

func testPutObject(t *testing.T, gateway *testGateway) {
	ctx := context.Background()
	type args struct {
		bucketName, objectName, objectData string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"OK-Bucket-Exists", args{testBucket1, testObject1, testObject1Data}, false},
		{"Fail-No-Bucket", args{testBucket2, testObject1, testObject1Data}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := gateway.PutObject(
				ctx, tt.args.bucketName, tt.args.objectName,
				getTestPutObjectReader(t, []byte(tt.args.objectData)),
				minio.ObjectOptions{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("PutObject() err %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && resp.Bucket != tt.args.bucketName {
				t.Fatal("bad bucket name")
			}
		})
	}
}

func testGetObject(t *testing.T, g *testGateway) {
	ctx := context.Background()
	buf := bytes.NewBuffer(nil)
	err := g.GetObject(ctx, testBucket1, testObject1, 0, 1, buf, "", minio.ObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 1 {
		t.Fatalf("unexpected read from object: %x", buf.Bytes())
	}
	err = g.GetObject(ctx, "fake bucket", testObject1, 0, 1, buf, "", minio.ObjectOptions{})
	if _, ok := err.(minio.BucketNotFound); !ok {
		t.Fatal("expected error BucketNotFound, but got", err)
	}
	err = g.GetObject(ctx, testBucket1, "fake object", 0, 1, buf, "", minio.ObjectOptions{})
	if _, ok := err.(minio.ObjectNotFound); !ok {
		t.Fatal("expected error ObjectNotFound, but got", err)
	}
	err = g.GetObject(ctx, testBucket1, testObject1, math.MaxInt64-1, 1, buf, "", minio.ObjectOptions{})
	if _, ok := err.(minio.InvalidRange); !ok {
		t.Fatal("expected error InvalidRange, but got", err)
	}
}

func TestS3XG_Object_Badger(t *testing.T) {
	testS3XGObject(t, DSTypeBadger, false)
}
func TestS3XG_Object_Badger_Passthrough(t *testing.T) {
	testS3XGObject(t, DSTypeBadger, true)
}
func TestS3XG_Object_Crdt(t *testing.T) {
	testS3XGObject(t, DSTypeCrdt, false)
}
func TestS3XG_Object_Crdt_Passthrough(t *testing.T) {
	testS3XGObject(t, DSTypeCrdt, true)
}
func testS3XGObject(t *testing.T, dsType DSType, passthrough bool) {
	ctx := context.Background()
	gateway := newTestGateway(t, dsType, passthrough)
	defer func() {
		if err := gateway.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
	}()
	type args struct {
		bucketName, objectName string
	}
	opts := minio.BucketOptions{
		Location: "us-east-1",
	}
	// setup test bucket
	if err := gateway.MakeBucketWithLocation(ctx, testBucket1, opts); err != nil {
		t.Fatal(err)
	}
	t.Run("PutObject", func(t *testing.T) {
		testPutObject(t, gateway)
	})
	t.Run("GetObject", func(t *testing.T) {
		testGetObject(t, gateway)
	})
	t.Run("GetObject from datastore", func(t *testing.T) {
		gateway.restart(t)
		testGetObject(t, gateway)
	})
	t.Run("ListObjects", func(t *testing.T) {
		tests := []struct {
			name    string
			args    args
			wantErr bool
		}{
			{"Fail-BucketNotExist", args{testBucket2, ""}, true},
			{"Success", args{testBucket1, ""}, false},
		}
		expectedLength := 1
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				list, err := gateway.ListObjects(
					ctx,
					tt.args.bucketName,
					"", "", "",
					500,
				)
				if (err != nil) != tt.wantErr {
					t.Fatalf("err %v, wantErr %v", err, tt.wantErr)
				}
				if err == nil && len(list.Objects) != expectedLength {
					t.Fatalf("got unexpected list: %+v", list)
				}
			})
			t.Run("V2/"+tt.name, func(t *testing.T) {
				list, err := gateway.ListObjectsV2(
					ctx,
					tt.args.bucketName,
					"", "", "",
					500, false, "",
				)
				if (err != nil) != tt.wantErr {
					t.Fatalf("err %v, wantErr %v", err, tt.wantErr)
				}
				if err == nil && len(list.Objects) != expectedLength {
					t.Fatalf("got unexpected list: %+v", list)
				}
			})
			t.Run("V2/startsAfter/"+tt.name, func(t *testing.T) {
				//test startsAfter
				list, err := gateway.ListObjectsV2(
					ctx,
					tt.args.bucketName,
					"", "", "",
					500, false, "x",
				)
				if (err != nil) != tt.wantErr {
					t.Fatalf("err %v, wantErr %v", err, tt.wantErr)
				}
				if err == nil && len(list.Objects) != 0 {
					t.Fatalf("got unexpected list: %v", list)
				}
			})
		}
	})
	t.Run("GetObjectInfo", func(t *testing.T) {
		tests := []struct {
			name    string
			args    args
			wantErr bool
		}{
			{"Ok", args{testBucket1, testObject1}, false},
			{"Fail-Bad-Object", args{testBucket1, "notarealobj"}, true},
			{"Fail-Bad-Bucket", args{"notarealbucket", testObject1}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				info, err := gateway.GetObjectInfo(
					ctx,
					tt.args.bucketName,
					tt.args.objectName,
					minio.ObjectOptions{},
				)
				if (err != nil) != tt.wantErr {
					t.Fatalf("GetObjectInfo() err %v, wantErr %v", err, tt.wantErr)
				}
				if tt.wantErr {
					return
				}
				if info.Bucket != tt.args.bucketName {
					t.Fatal("bad bucket")
				}
				if info.Name != tt.args.objectName {
					t.Fatal("bad object")
				}
			})
		}

	})
	t.Run("ListObjectsV2", func(t *testing.T) {
		t.Skip()
	})
	t.Run("GetObjectNInfo", func(t *testing.T) {
		tests := []struct {
			name    string
			args    args
			wantErr bool
		}{
			{"Ok", args{testBucket1, testObject1}, false},
			{"Fail-Bad-Object", args{testBucket1, "notarealobj"}, true},
			{"Fail-Bad-Bucket", args{"notarealbucket", testObject1}, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp, err := gateway.GetObjectNInfo(
					ctx,
					tt.args.bucketName,
					tt.args.objectName,
					&minio.HTTPRangeSpec{},
					nil,
					0,
					minio.ObjectOptions{},
				)
				if (err != nil) != tt.wantErr {
					t.Fatalf("GetObjectNInfo() err %v, wantErr %v", err, tt.wantErr)
				}
				if tt.wantErr {
					return
				}
				if resp.ObjInfo.Bucket != tt.args.bucketName {
					t.Fatal("bad bucket")
				}
				if resp.ObjInfo.Name != tt.args.objectName {
					t.Fatal("bad object")
				}
				err = resp.Close()
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	})
	t.Run("CopyObject", func(t *testing.T) {
		dstBucket := "dstBucket"
		dstObject := "dstObject"
		err := gateway.MakeBucketWithLocation(ctx, dstBucket, opts)
		if err != nil {
			t.Fatal(err)
		}
		info, err := gateway.CopyObject(ctx, testBucket1, testObject1, dstBucket, dstObject, minio.ObjectInfo{}, minio.ObjectOptions{}, minio.ObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if info.Bucket != dstBucket {
			t.Fatal("expected destination bucket, got:", info.Bucket)
		}
		if info.Name != dstObject {
			t.Fatal("expected destination object name, got:", info.Name)
		}
	})
	t.Run("DeleteObject", func(t *testing.T) {
		opts := minio.ObjectOptions{}
		_, err := gateway.DeleteObject(ctx, testBucket1, testObject1, opts)
		if err != nil {
			t.Fatal(err)
		}
		_, err = gateway.DeleteObject(ctx, testBucket1, testObject1, opts)
		if _, ok := err.(minio.ObjectNotFound); !ok {
			t.Fatal("expected err ObjectNotFound, but got: ", err)
		}
		gateway.restart(t)
		//conform that we deleted from datastore
		_, err = gateway.DeleteObject(ctx, testBucket1, testObject1, opts)
		if _, ok := err.(minio.ObjectNotFound); !ok {
			t.Fatal("expected err ObjectNotFound, but got: ", err)
		}
	})
	t.Run("DeleteObjects", func(t *testing.T) {
		testPutObject(t, gateway) // put object back before testing delete
		objects := []minio.ObjectToDelete{
			{ObjectName: testObject1},
			{ObjectName: "not an object"},
		}
		opts := minio.ObjectOptions{}
		deletes, errs := gateway.DeleteObjects(ctx, testBucket1, objects, opts)
		if len(deletes) != len(objects) {
			t.Fatal("unexpected number of deletes:", deletes)
		}
		if len(errs) != len(objects) {
			t.Fatal("unexpected number of deletes:", errs)
		}
		for i := range deletes {
			err := errs[i]
			if i == 0 {
				if err != nil {
					t.Fatal("unexpected err:", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected err but not nil")
				}
			}
		}
	})
}

func getTestHashReader(t testing.TB, input io.Reader, size int64) *hash.Reader {
	r, err := hash.NewReader(input, size, "", "", size, false)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func getTestPutObjectReader(t testing.TB, data []byte) *minio.PutObjReader {
	return minio.NewPutObjReader(
		getTestHashReader(
			t,
			bytes.NewReader(data),
			int64(len(data)),
		), nil, nil)
}
