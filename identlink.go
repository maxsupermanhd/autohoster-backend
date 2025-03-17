package main

import (
	"context"
	"errors"
	"runtime/debug"
	"strings"
	"time"

	"github.com/jackc/pgx/v4"
)

func processLinkingMessage(inst *instance, fromPk []byte, fromPkBase64 string, playername string, code string) {
	confirmCode := strings.TrimPrefix(strings.Trim(code, " 	\n\r"), "/hostmsg ")

	if !strings.HasPrefix(confirmCode, "confirm-") {
		return
	}

	ctx, ctxCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ctxCancel()

	var accountID int
	var emailConfirmed *time.Time
	err := dbpool.QueryRow(ctx, `select id, email_confirmed from accounts where wz_confirm_code = $1`, confirmCode).Scan(&accountID, &emailConfirmed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			instWriteFmt(inst, `chat direct %s %s`, fromPkBase64, "⚠ Invalid code, please get one at https://wz2100-autohost.net/wzlink")
			return
		} else {
			instWriteFmt(inst, `chat direct %s %s`, fromPkBase64, "⚠ Something went wrong, contact administrators for assistance.")
			discordPostError(`%s\n%s`, err.Error(), string(debug.Stack()))
			return
		}
	}

	if emailConfirmed == nil {
		instWriteFmt(inst, `chat direct %s %s`, fromPkBase64, "⚠ Email not confirmed. Please confirm your email to link an identity. If you need to re-send confirmation email or change your address contact Administrators.")
		return
	}

	err = dbpool.BeginFunc(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `insert into identities (name, pkey, hash, account, claimed_at)
		values ($1, $2, encode(sha256($2), 'hex'), $3, now())
		on conflict (hash) do update set account = $3, claimed_at = now() where identities.account is null and identities.pkey = $2`,
			playername, fromPk, accountID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			instWriteFmt(inst, `chat direct %s %s`, fromPkBase64, "⚠ Identity already claimed, contact administrators if you are confused.")
			return nil
		}
		if tag.RowsAffected() > 1 {
			instWriteFmt(inst, `chat direct %s %s`, fromPkBase64, "⚠ Something went wrong, contact administrators for assistance.")
			discordPostError(`sus tag %s on identity insert\n%s`, tag, string(debug.Stack()))
			return nil
		}
		tag, err = tx.Exec(ctx, `update accounts set wz_confirm_code = null, display_name = $1 where id = $2`, playername, accountID)
		if err != nil {
			return err
		}
		if !tag.Update() || tag.RowsAffected() != 1 {
			discordPostError(`sus tag %s on account confirm code clear while linking\n%s`, tag, string(debug.Stack()))
		}
		instWriteFmt(inst, `chat direct %s %s`, fromPkBase64, "☑ Identity linked successfully")
		return nil
	})
	if err != nil {
		instWriteFmt(inst, `chat direct %s %s`, fromPkBase64, "⚠ Something went wrong, contact administrators for assistance.")
		discordPostError(`identity linking tx error %s\n%s`, err.Error(), string(debug.Stack()))
		return
	}
}
