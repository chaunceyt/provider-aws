/*
Copyright 2019 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package iamgrouppolicyattachment

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	runtimev1alpha1 "github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"

	"github.com/crossplane/provider-aws/apis/identity/v1alpha1"
	awsv1alpha3 "github.com/crossplane/provider-aws/apis/v1alpha3"
	awsclients "github.com/crossplane/provider-aws/pkg/clients"
	"github.com/crossplane/provider-aws/pkg/clients/iam"
	"github.com/crossplane/provider-aws/pkg/clients/iam/fake"
)

const (
	providerName    = "aws-creds"
	secretNamespace = "crossplane-system"
	testRegion      = "us-east-1"

	connectionSecretName = "my-little-secret"
	secretKey            = "credentials"
	credData             = "confidential!"
)

var (
	unexpectedItem resource.Managed
	policyArn      = "some arn"
	groupName      = "some group"

	errBoom = errors.New("boom")
)

type args struct {
	iam iam.GroupPolicyAttachmentClient
	cr  resource.Managed
}

type groupPolicyModifier func(*v1alpha1.IAMGroupPolicyAttachment)

func withConditions(c ...runtimev1alpha1.Condition) groupPolicyModifier {
	return func(r *v1alpha1.IAMGroupPolicyAttachment) { r.Status.ConditionedStatus.Conditions = c }
}

func withGroupName(s *string) groupPolicyModifier {
	return func(r *v1alpha1.IAMGroupPolicyAttachment) { r.Spec.ForProvider.GroupName = s }
}

func withSpecPolicyArn(s string) groupPolicyModifier {
	return func(r *v1alpha1.IAMGroupPolicyAttachment) { r.Spec.ForProvider.PolicyARN = &s }
}

func withStatusPolicyArn(s string) groupPolicyModifier {
	return func(r *v1alpha1.IAMGroupPolicyAttachment) { r.Status.AtProvider.AttachedPolicyARN = s }
}

func groupPolicy(m ...groupPolicyModifier) *v1alpha1.IAMGroupPolicyAttachment {
	cr := &v1alpha1.IAMGroupPolicyAttachment{
		Spec: v1alpha1.IAMGroupPolicyAttachmentSpec{
			ResourceSpec: runtimev1alpha1.ResourceSpec{
				ProviderReference: runtimev1alpha1.Reference{Name: providerName},
			},
		},
	}
	for _, f := range m {
		f(cr)
	}
	return cr
}

func TestConnect(t *testing.T) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      connectionSecretName,
			Namespace: secretNamespace,
		},
		Data: map[string][]byte{
			secretKey: []byte(credData),
		},
	}

	providerSA := func(saVal bool) awsv1alpha3.Provider {
		return awsv1alpha3.Provider{
			Spec: awsv1alpha3.ProviderSpec{
				Region:            testRegion,
				UseServiceAccount: &saVal,
				ProviderSpec: runtimev1alpha1.ProviderSpec{
					CredentialsSecretRef: &runtimev1alpha1.SecretKeySelector{
						SecretReference: runtimev1alpha1.SecretReference{
							Namespace: secretNamespace,
							Name:      connectionSecretName,
						},
						Key: secretKey,
					},
				},
			},
		}
	}
	type args struct {
		kube        client.Client
		newClientFn func(ctx context.Context, credentials []byte, region string, auth awsclients.AuthMethod) (iam.GroupPolicyAttachmentClient, error)
		cr          *v1alpha1.IAMGroupPolicyAttachment
	}
	type want struct {
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"Successful": {
			args: args{
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						switch key {
						case client.ObjectKey{Name: providerName}:
							p := providerSA(false)
							p.DeepCopyInto(obj.(*awsv1alpha3.Provider))
							return nil
						case client.ObjectKey{Namespace: secretNamespace, Name: connectionSecretName}:
							secret.DeepCopyInto(obj.(*corev1.Secret))
							return nil
						}
						return errBoom
					},
				},
				newClientFn: func(_ context.Context, credentials []byte, region string, _ awsclients.AuthMethod) (i iam.GroupPolicyAttachmentClient, e error) {
					if diff := cmp.Diff(credData, string(credentials)); diff != "" {
						t.Errorf("r: -want, +got:\n%s", diff)
					}
					if diff := cmp.Diff(testRegion, region); diff != "" {
						t.Errorf("r: -want, +got:\n%s", diff)
					}
					return nil, nil
				},
				cr: groupPolicy(),
			},
		},
		"SuccessfulUseServiceAccount": {
			args: args{
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						if key == (client.ObjectKey{Name: providerName}) {
							p := providerSA(true)
							p.DeepCopyInto(obj.(*awsv1alpha3.Provider))
							return nil
						}
						return errBoom
					},
				},
				newClientFn: func(_ context.Context, credentials []byte, region string, _ awsclients.AuthMethod) (i iam.GroupPolicyAttachmentClient, e error) {
					if diff := cmp.Diff("", string(credentials)); diff != "" {
						t.Errorf("r: -want, +got:\n%s", diff)
					}
					if diff := cmp.Diff(testRegion, region); diff != "" {
						t.Errorf("r: -want, +got:\n%s", diff)
					}
					return nil, nil
				},
				cr: groupPolicy(),
			},
		},
		"ProviderGetFailed": {
			args: args{
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						return errBoom
					},
				},
				cr: groupPolicy(),
			},
			want: want{
				err: errors.Wrap(errBoom, errGetProvider),
			},
		},
		"SecretGetFailed": {
			args: args{
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						switch key {
						case client.ObjectKey{Name: providerName}:
							p := providerSA(false)
							p.DeepCopyInto(obj.(*awsv1alpha3.Provider))
							return nil
						case client.ObjectKey{Namespace: secretNamespace, Name: connectionSecretName}:
							return errBoom
						default:
							return nil
						}
					},
				},
				cr: groupPolicy(),
			},
			want: want{
				err: errors.Wrap(errBoom, errGetProviderSecret),
			},
		},
		"SecretGetFailedNil": {
			args: args{
				kube: &test.MockClient{
					MockGet: func(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
						switch key {
						case client.ObjectKey{Name: providerName}:
							p := providerSA(false)
							p.SetCredentialsSecretReference(nil)
							p.DeepCopyInto(obj.(*awsv1alpha3.Provider))
							return nil
						case client.ObjectKey{Namespace: secretNamespace, Name: connectionSecretName}:
							return errBoom
						default:
							return nil
						}
					},
				},
				cr: groupPolicy(),
			},
			want: want{
				err: errors.New(errGetProviderSecret),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := &connector{kube: tc.kube, newClientFn: tc.newClientFn}
			_, err := c.Connect(context.Background(), tc.args.cr)
			if diff := cmp.Diff(tc.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("r: -want, +got:\n%s", diff)
			}
		})
	}
}

func TestObserve(t *testing.T) {

	type want struct {
		cr     resource.Managed
		result managed.ExternalObservation
		err    error
	}

	cases := map[string]struct {
		args
		want
	}{
		"VaildInput": {
			args: args{
				iam: &fake.MockGroupPolicyAttachmentClient{
					MockListAttachedGroupPolicies: func(input *awsiam.ListAttachedGroupPoliciesInput) awsiam.ListAttachedGroupPoliciesRequest {
						return awsiam.ListAttachedGroupPoliciesRequest{
							Request: &aws.Request{HTTPRequest: &http.Request{}, Retryer: aws.NoOpRetryer{}, Data: &awsiam.ListAttachedGroupPoliciesOutput{
								AttachedPolicies: []awsiam.AttachedPolicy{
									{
										PolicyArn: &policyArn,
									},
								},
							}},
						}
					},
				},
				cr: groupPolicy(withGroupName(&groupName),
					withSpecPolicyArn(policyArn)),
			},
			want: want{
				cr: groupPolicy(withGroupName(&groupName),
					withSpecPolicyArn(policyArn),
					withConditions(runtimev1alpha1.Available()),
					withStatusPolicyArn(policyArn)),
				result: managed.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: true,
				},
			},
		},
		"InValidInput": {
			args: args{
				cr: unexpectedItem,
			},
			want: want{
				cr:  unexpectedItem,
				err: errors.New(errUnexpectedObject),
			},
		},
		"NoAttachedPolicy": {
			args: args{
				iam: &fake.MockGroupPolicyAttachmentClient{
					MockListAttachedGroupPolicies: func(input *awsiam.ListAttachedGroupPoliciesInput) awsiam.ListAttachedGroupPoliciesRequest {
						return awsiam.ListAttachedGroupPoliciesRequest{
							Request: &aws.Request{HTTPRequest: &http.Request{}, Retryer: aws.NoOpRetryer{}, Data: &awsiam.ListAttachedGroupPoliciesOutput{}},
						}
					},
				},
				cr: groupPolicy(withSpecPolicyArn(policyArn)),
			},
			want: want{
				cr: groupPolicy(withSpecPolicyArn(policyArn)),
			},
		},
		"ClientError": {
			args: args{
				iam: &fake.MockGroupPolicyAttachmentClient{
					MockListAttachedGroupPolicies: func(input *awsiam.ListAttachedGroupPoliciesInput) awsiam.ListAttachedGroupPoliciesRequest {
						return awsiam.ListAttachedGroupPoliciesRequest{
							Request: &aws.Request{HTTPRequest: &http.Request{}, Retryer: aws.NoOpRetryer{}, Error: errBoom},
						}
					},
				},
				cr: groupPolicy(withGroupName(&groupName)),
			},
			want: want{
				cr:  groupPolicy(withGroupName(&groupName)),
				err: errors.Wrap(errBoom, errGet),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := &external{client: tc.iam}
			o, err := e.Observe(context.Background(), tc.args.cr)

			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("r: -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.cr, tc.args.cr, test.EquateConditions()); diff != "" {
				t.Errorf("r: -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.result, o); diff != "" {
				t.Errorf("r: -want, +got:\n%s", diff)
			}
		})
	}
}

func TestCreate(t *testing.T) {

	type want struct {
		cr     resource.Managed
		result managed.ExternalCreation
		err    error
	}

	cases := map[string]struct {
		args
		want
	}{
		"VaildInput": {
			args: args{
				iam: &fake.MockGroupPolicyAttachmentClient{
					MockAttachGroupPolicy: func(input *awsiam.AttachGroupPolicyInput) awsiam.AttachGroupPolicyRequest {
						return awsiam.AttachGroupPolicyRequest{
							Request: &aws.Request{HTTPRequest: &http.Request{}, Retryer: aws.NoOpRetryer{}, Data: &awsiam.AttachGroupPolicyOutput{}},
						}
					},
				},
				cr: groupPolicy(withGroupName(&groupName),
					withSpecPolicyArn(policyArn)),
			},
			want: want{
				cr: groupPolicy(
					withGroupName(&groupName),
					withSpecPolicyArn(policyArn),
					withConditions(runtimev1alpha1.Creating())),
			},
		},
		"InValidInput": {
			args: args{
				cr: unexpectedItem,
			},
			want: want{
				cr:  unexpectedItem,
				err: errors.New(errUnexpectedObject),
			},
		},
		"ClientError": {
			args: args{
				iam: &fake.MockGroupPolicyAttachmentClient{
					MockAttachGroupPolicy: func(input *awsiam.AttachGroupPolicyInput) awsiam.AttachGroupPolicyRequest {
						return awsiam.AttachGroupPolicyRequest{
							Request: &aws.Request{HTTPRequest: &http.Request{}, Retryer: aws.NoOpRetryer{}, Error: errBoom},
						}
					},
				},
				cr: groupPolicy(withGroupName(&groupName),
					withSpecPolicyArn(policyArn)),
			},
			want: want{
				cr: groupPolicy(withGroupName(&groupName),
					withSpecPolicyArn(policyArn),
					withConditions(runtimev1alpha1.Creating())),
				err: errors.Wrap(errBoom, errAttach),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := &external{client: tc.iam}
			o, err := e.Create(context.Background(), tc.args.cr)

			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("r: -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.cr, tc.args.cr, test.EquateConditions()); diff != "" {
				t.Errorf("r: -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.result, o); diff != "" {
				t.Errorf("r: -want, +got:\n%s", diff)
			}
		})
	}
}

func TestDelete(t *testing.T) {

	type want struct {
		cr  resource.Managed
		err error
	}

	cases := map[string]struct {
		args
		want
	}{
		"VaildInput": {
			args: args{
				iam: &fake.MockGroupPolicyAttachmentClient{
					MockDetachGroupPolicy: func(input *awsiam.DetachGroupPolicyInput) awsiam.DetachGroupPolicyRequest {
						return awsiam.DetachGroupPolicyRequest{
							Request: &aws.Request{HTTPRequest: &http.Request{}, Retryer: aws.NoOpRetryer{}, Data: &awsiam.DetachGroupPolicyOutput{}},
						}
					},
				},
				cr: groupPolicy(withGroupName(&groupName),
					withSpecPolicyArn(policyArn)),
			},
			want: want{
				cr: groupPolicy(
					withGroupName(&groupName),
					withSpecPolicyArn(policyArn),
					withConditions(runtimev1alpha1.Deleting())),
			},
		},
		"InValidInput": {
			args: args{
				cr: unexpectedItem,
			},
			want: want{
				cr:  unexpectedItem,
				err: errors.New(errUnexpectedObject),
			},
		},
		"ClientError": {
			args: args{
				iam: &fake.MockGroupPolicyAttachmentClient{
					MockDetachGroupPolicy: func(input *awsiam.DetachGroupPolicyInput) awsiam.DetachGroupPolicyRequest {
						return awsiam.DetachGroupPolicyRequest{
							Request: &aws.Request{HTTPRequest: &http.Request{}, Retryer: aws.NoOpRetryer{}, Error: errBoom},
						}
					},
				},
				cr: groupPolicy(withGroupName(&groupName),
					withSpecPolicyArn(policyArn)),
			},
			want: want{
				cr: groupPolicy(withGroupName(&groupName),
					withSpecPolicyArn(policyArn),
					withConditions(runtimev1alpha1.Deleting())),
				err: errors.Wrap(errBoom, errDetach),
			},
		},
		"ResourceDoesNotExist": {
			args: args{
				iam: &fake.MockGroupPolicyAttachmentClient{
					MockDetachGroupPolicy: func(input *awsiam.DetachGroupPolicyInput) awsiam.DetachGroupPolicyRequest {
						return awsiam.DetachGroupPolicyRequest{
							Request: &aws.Request{HTTPRequest: &http.Request{}, Error: errors.New(errGet)},
						}
					},
				},
				cr: groupPolicy(),
			},
			want: want{
				cr:  groupPolicy(withConditions(runtimev1alpha1.Deleting())),
				err: errors.Wrap(errors.New(errGet), errDetach),
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := &external{client: tc.iam}
			err := e.Delete(context.Background(), tc.args.cr)

			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("r: -want, +got:\n%s", diff)
			}
			if diff := cmp.Diff(tc.want.cr, tc.args.cr, test.EquateConditions()); diff != "" {
				t.Errorf("r: -want, +got:\n%s", diff)
			}
		})
	}
}
