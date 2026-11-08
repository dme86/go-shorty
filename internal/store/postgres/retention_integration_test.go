package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/example/shorty/internal/shortid"
	"github.com/example/shorty/internal/store"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Run with SHORTY_TEST_DATABASE_URL pointing to a disposable database that
// already has all goose migrations applied.
func TestPostgresDedupeCollisionAndClickRetention(t *testing.T) {
	dsn:=os.Getenv("SHORTY_TEST_DATABASE_URL")
	if dsn=="" { t.Skip("SHORTY_TEST_DATABASE_URL not set") }
	db,err:=sql.Open("pgx",dsn)
	if err!=nil {t.Fatal(err)}
	defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	if err:=db.PingContext(ctx);err!=nil {t.Fatal(err)}
	p:=New(db)
	tenant:="domain:regression.example"
	url:="https://example.com/r?q=2026"
	newCode:=func()string{
		id,err:=shortid.Random(7)
		if err!=nil {t.Fatal(err)}
		return id
	}
	create:=func(code string,max *int64,exp *time.Time){
		l:=store.Link{Code:code,LongURL:url,IsActive:true}
		if max!=nil {l.MaxClicks=sql.NullInt64{Valid:true,Int64:*max}}
		if exp!=nil {l.ExpiresAt=sql.NullTime{Valid:true,Time:*exp}}
		if err:=p.CreateLink(ctx,tenant,l);err!=nil {t.Fatal(err)}
		t.Cleanup(func(){
			cleanupCtx,done:=context.WithTimeout(context.Background(),10*time.Second)
			defer done()
			_,_ = db.ExecContext(cleanupCtx,"DELETE FROM links WHERE code=$1",code)
		})
	}
	a,b,c:=newCode(),newCode(),newCode()
	max3,max4:=int64(3),int64(4)
	exp:=time.Now().UTC().Add(24*time.Hour).Truncate(time.Microsecond)
	create(a,&max3,nil)
	create(b,&max4,nil)
	create(c,&max3,&exp)

	for _,tc:=range []struct{
		name string
		max *int64
		exp *time.Time
		want string
	}{
		{"max3",&max3,nil,a},
		{"max4",&max4,nil,b},
		{"max3 with expiry",&max3,&exp,c},
	}{
		t.Run(tc.name,func(t *testing.T){
			got,err:=p.FindActiveByLongURL(ctx,tenant,url,tc.exp,tc.max)
			if err!=nil||got.Code!=tc.want {t.Fatalf("dedupe got=%q err=%v want=%q",got.Code,err,tc.want)}
		})
	}
	if _,err:=p.FindActiveByLongURL(ctx,tenant,url,nil,nil);!errors.Is(err,store.ErrNotFound) {
		t.Fatalf("unlimited settings unexpectedly matched a limited link: %v",err)
	}
	// The primary key is global, not per tenant.
	if err:=p.CreateLink(ctx,"domain:another.example",store.Link{Code:a,LongURL:"https://elsewhere.example",IsActive:true});!errors.Is(err,store.ErrCodeTaken){
		t.Fatalf("cross-tenant code collision was not enforced: %v",err)
	}

	_,err=db.ExecContext(ctx,"UPDATE links SET click_count=2 WHERE code=$1",a)
	if err!=nil {t.Fatal(err)}
	old:=time.Now().UTC().AddDate(0,0,-110)
	recent:=time.Now().UTC().AddDate(0,0,-5)
	_,err=db.ExecContext(ctx,"INSERT INTO clicks (tenant_id,code,ts,ua,referer) VALUES ($1,$2,$3,'desktop','https://old.example'),($1,$2,$4,'mobile','https://new.example')",tenant,a,old,recent)
	if err!=nil {t.Fatal(err)}
	deleted,err:=p.DeleteOldClicks(ctx,time.Now().UTC().AddDate(0,0,-90),1)
	if err!=nil||deleted!=1 {t.Fatalf("retention deleted %d, err=%v",deleted,err)}
	// Lifetime count survives; chart data only covers retained detail.
	stats,err:=p.GetStats(ctx,tenant,a)
	if err!=nil||stats.ClickCount!=2||len(stats.ClicksPerDay)!=1||stats.ClicksPerDay[0].Clicks!=1{
		t.Fatalf("stats after retention: %+v err=%v",stats,err)
	}
	ok,err:=p.TryIncrementClick(ctx,tenant,a,"ua","", "")
	if err!=nil||!ok {t.Fatalf("last click denied: %v",err)}
	ok,err=p.TryIncrementClick(ctx,tenant,a,"ua","", "")
	if err!=nil||ok {t.Fatalf("max-click guard failed: allowed=%v err=%v",ok,err)}
	var total,raw int64
	if err:=db.QueryRowContext(ctx,"SELECT click_count FROM links WHERE code=$1",a).Scan(&total);err!=nil {t.Fatal(err)}
	if err:=db.QueryRowContext(ctx,"SELECT COUNT(*) FROM clicks WHERE code=$1",a).Scan(&raw);err!=nil {t.Fatal(err)}
	if total!=3||raw!=2 {t.Fatalf("retention corrupted counters: total=%d raw=%d",total,raw)}
	if _,err:=p.DeleteOldClicks(ctx,time.Now().UTC(),0);err==nil {t.Fatal("invalid batch size accepted")}
}
