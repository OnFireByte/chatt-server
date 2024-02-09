package hub

func createUserChatKey(user1, user2 string) string {
	if user1 < user2 {
		return "user:" + user1 + ":" + user2
	}
	return "user:" + user2 + ":" + user1
}
