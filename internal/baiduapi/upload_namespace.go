package baiduapi

import "context"

// UploadNamespaceAdmissionGuard holds local batch admission until the caller
// saves its remote-operation intent. Release receives the persistence error
// and must run before network I/O, including on failed intent persistence.
type UploadNamespaceAdmissionGuard func(target, operationID string) (release func(error), err error)
type uploadNamespaceAdmissionKey struct{}

func WithUploadNamespaceAdmissionGuard(ctx context.Context, guard UploadNamespaceAdmissionGuard) context.Context {
	return context.WithValue(ctx, uploadNamespaceAdmissionKey{}, guard)
}

func AcquireUploadNamespaceAdmission(ctx context.Context, target, operationID string) (func(error), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	guard, _ := ctx.Value(uploadNamespaceAdmissionKey{}).(UploadNamespaceAdmissionGuard)
	if guard == nil {
		return func(error) {}, nil
	}
	return guard(target, operationID)
}
